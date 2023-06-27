package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

//go:embed web/*
var web embed.FS

type Server struct {
	games        map[uuid.UUID]*Game
	gamesMu      sync.Mutex
	mux          *http.ServeMux
	gameTemplate *template.Template
}

func NewServer() *Server {
	return &Server{
		gameTemplate: template.Must(template.ParseFS(web, "web/*.html")),
		games:        make(map[uuid.UUID]*Game),
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
	for _, game := range s.games {
		if game.EndedAt != nil {
			s.gamesMu.Lock()
			delete(s.games, game.Id)
			s.gamesMu.Unlock()
		}
	}

	if len(s.games) > 1000 {
		log.Printf("too many games in progress")
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte("too many games in progress"))
		return
	}

	game := NewGame()
	s.gamesMu.Lock()
	s.games[game.Id] = game
	s.gamesMu.Unlock()
	redirectUrl := "/game?id=" + game.Id.String()
	log.Printf("created game %s...\n", game.Id)
	http.Redirect(w, r, redirectUrl, http.StatusPermanentRedirect)
}

func (s *Server) getGameFromRequest(r *http.Request) (*Game, error) {
	idParam := r.URL.Query().Get("id")
	if idParam == "" {
		return nil, errors.New("id param is missing")
	}

	id, err := uuid.Parse(idParam)
	if err != nil {
		return nil, fmt.Errorf("invalid id %s: %w", idParam, err)
	}

	game, ok := s.games[id]
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
		GameId uuid.UUID
		Error  error
	}{
		GameId: game.Id,
		Error:  err,
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
