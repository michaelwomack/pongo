package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"sync"

	"nhooyr.io/websocket"
)

//go:embed web/*
var web embed.FS

type Server struct {
	games        map[string]*Game
	gamesMu      sync.Mutex
	mux          *http.ServeMux
	gameTemplate *template.Template
}

func NewServer() *Server {
	return &Server{
		gameTemplate: template.Must(template.ParseFS(web, "web/*.html")),
		games:        make(map[string]*Game),
		mux:          http.NewServeMux(),
	}
}

func (s *Server) run() {
	webSys, err := fs.Sub(web, "web")
	if err != nil {
		panic(err)
	}
	s.mux.Handle("/", http.FileServer(http.FS(webSys)))
	s.mux.HandleFunc("/ws", s.handleWebsocket)
	s.mux.HandleFunc("/game/new", s.handleNewGame)
	s.mux.HandleFunc("/game", s.handleGame)
	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}
	log.Printf("listening on port %s...\n", port)
	if err := http.ListenAndServe(":"+port, s.mux); err != nil {
		log.Printf("failed to listen on server: %s\n", err)
	}
}

func (s *Server) handleNewGame(w http.ResponseWriter, r *http.Request) {
	type response struct {
		Message string `json:"message"`
	}

	w.Header().Set("Content-Type", "application/json")
	toMessageBytes := func(message string) []byte {
		b, err := json.Marshal(response{Message: message})
		if err != nil {
			log.Printf("error marshalling %s", message)
		}
		return b
	}

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	for _, game := range s.games {
		if game.EndedAt != nil {
			s.gamesMu.Lock()
			delete(s.games, game.Code)
			s.gamesMu.Unlock()
		}
	}

	if len(s.games) > 100 {
		w.WriteHeader(http.StatusConflict)
		w.Write(toMessageBytes("too many games in progress"))
		return
	}

	var body struct {
		Code string `json:"code"`
	}

	b, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("failed to read request body: %v\n", err)
		return
	}

	if err := json.Unmarshal(b, &body); err != nil {
		log.Printf("error unmarshalling body %s: %s\n", b, err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if body.Code == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write(toMessageBytes("missing game code"))
		return
	}

	if _, ok := s.games[body.Code]; ok {
		w.WriteHeader(http.StatusBadRequest)
		w.Write(toMessageBytes(fmt.Sprintf("'%s' is already taken. Try another.", body.Code)))
		return
	}

	game := NewGame(body.Code)
	s.gamesMu.Lock()
	s.games[game.Code] = game
	s.gamesMu.Unlock()
	log.Printf("created game %s with id %s...\n", game.Code, game.Id)
}

func (s *Server) getGameFromRequest(r *http.Request) (*Game, error) {
	gameCode := r.URL.Query().Get("code")
	if gameCode == "" {
		return nil, errors.New("game code param is missing")
	}

	game, ok := s.games[gameCode]
	if !ok {
		return nil, errors.New("game doesn't exist")
	}
	return game, nil
}

func (s *Server) handleGame(w http.ResponseWriter, r *http.Request) {
	game, err := s.getGameFromRequest(r)
	if err != nil || game == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var data = struct {
		GameCode string
		Error    error
	}{
		GameCode: game.Code,
		Error:    err,
	}
	if err := s.gameTemplate.Execute(w, data); err != nil {
		log.Printf("failed to execute game %s page: %s\n", game.Id, err)
	}
}

func (s *Server) handleWebsocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Printf("failed to connect websocket: %s\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("failed to connect websocket"))
		return
	}

	game, err := s.getGameFromRequest(r)
	if err != nil {
		log.Printf("failed to get game from request: %s\n", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("unable to connect to specified game"))
		return
	}

	ctx := context.Background()
	client := NewClient()
	game.assignPlayer(client)
	client.stream(ctx, conn)
	if game.isGameReady() {
		go game.run()
	}
}

func main() {
	server := NewServer()
	server.run()
}
