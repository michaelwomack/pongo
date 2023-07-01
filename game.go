package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

const (
	framesPerSecond     = 60
	gameHeight          = 800
	gameWidth           = 1600
	gameDurationSeconds = 120
	paddleWallPadding   = 8
	paddleWidth         = 15
	paddleHeight        = 100
	hitStreak           = 10
	maxBallDx           = 9
)

type Ball struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Dx     int `json:"dx"`
	Dy     int `json:"dy"`
	Radius int `json:"radius"`
}

func (b *Ball) increaseSpeed() {
	if b.Dx < 0 {
		b.Dx--
	} else if b.Dx > 0 {
		b.Dx++
	}
}

func (b *Ball) diameter() int {
	return b.Radius * 2
}

func (b *Ball) update() {
	b.X += b.Dx
	b.Y += b.Dy
}

// Paddle will only be updated from the clients.
type Paddle struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Dx     int `json:"dx"`
	Dy     int `json:"dy"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

func NewPaddle(isLeft bool) *Paddle {
	paddle := &Paddle{
		Y:      gameHeight/2 - (paddleHeight / 2),
		Width:  paddleWidth,
		Height: paddleHeight,
		Dx:     0,
		Dy:     0,
	}
	if isLeft {
		paddle.X = paddleWallPadding
	} else {
		paddle.X = gameWidth - paddleWidth - paddleWallPadding
	}
	return paddle
}

func (p *Paddle) update(game *Game) {
	if p == nil {
		return
	}

	p.Y += p.Dy

	if p.Y <= paddleWallPadding {
		p.Y = paddleWallPadding
	}

	if p.Y+p.Height > game.Height-paddleWallPadding {
		p.Y = game.Height - p.Height - paddleWallPadding
	}
}

type Client struct {
	conn     *websocket.Conn
	outbound chan []byte // data pushed to this client
	mu       sync.Mutex
	exit     chan struct{}
	Id       uuid.UUID `json:"id"`
	Paddle   *Paddle   `json:"paddle"`
	IsLeft   bool      `json:"isLeft"`
	Score    int       `json:"score"`
}

func NewClient() *Client {
	return &Client{
		Id:       uuid.New(),
		conn:     nil,
		outbound: make(chan []byte),
		mu:       sync.Mutex{},
		exit:     make(chan struct{}),
		Paddle:   nil,
	}
}

func (c *Client) stream(ctx context.Context, conn *websocket.Conn) {
	c.conn = conn
	go c.write(ctx) // start writing data to the client
	go c.read(ctx)  // continuously read input from the client
	data, err := json.Marshal(NewMessageClientConnected(c))
	if err != nil {
		log.Printf("failed to send connect message: %s\n", err)
		return
	}
	c.pushBytes(data) // send the first message indicating that we're connected
}

// write continually polls for game state data to send
// to the client.
func (c *Client) write(ctx context.Context) {
	log.Printf("client %s writing...\n", c.Id)
	write := true
	for write {
		select {
		case <-ctx.Done():
			log.Printf("client.write done: %s\n", ctx.Err())
			write = false
		case outbound := <-c.outbound:
			if err := c.conn.Write(ctx, websocket.MessageText, outbound); err != nil {
				log.Printf("failed to write data %s to client %s: %s\n", outbound, c.Id, err)
				write = false
			}
		}
	}
	c.exit <- struct{}{}
	log.Printf("client %s has exited writing...\n", c.Id)
}

// push sends this byte data to the client. This may block.
func (c *Client) pushBytes(msg []byte) {
	c.outbound <- msg
}

func (c *Client) push(msg any) {
	b, err := json.Marshal(msg)
	if err != nil {
		log.Printf("client %s failed to marshal %v: %s\n", c.Id, msg, err)
		return
	}
	c.pushBytes(b)
}

// read consumes this byte data from the client. We won't
// worry about this for now.
func (c *Client) read(ctx context.Context) {
	log.Printf("client %s is reading...\n", c.Id)
	for {
		var input MessagePlayerInput
		if err := wsjson.Read(ctx, c.conn, &input); err != nil {
			log.Printf("error reading from %s connection: %s: status: %+v\n", c.Id, err, websocket.CloseStatus(err))
			break
		}
		c.Paddle = input.Paddle
	}
	c.exit <- struct{}{}
	log.Printf("client %s has exited reading...\n", c.Id)
}

type Game struct {
	Id        uuid.UUID
	Code      string
	Width     int
	Height    int
	Player1   *Client
	Player2   *Client
	Ball      *Ball
	Collision bool
	Streak    int // number of paddle hits in a row
	Done      chan struct{}
	StartedAt *time.Time
	EndedAt   *time.Time
}

func NewGame(code string) *Game {
	return &Game{
		Id:     uuid.New(),
		Code:   code,
		Width:  gameWidth,
		Height: gameHeight,
		Ball: &Ball{
			X:      gameWidth / 2,
			Y:      gameHeight / 2,
			Dx:     6,
			Dy:     2,
			Radius: 10,
		},
	}
}

func (g *Game) assignPlayer(client *Client) {
	if g.Player1 == nil {
		client.IsLeft = true
		g.Player1 = client
	} else if g.Player2 == nil {
		g.Player2 = client
	}
	client.Paddle = NewPaddle(client.IsLeft)
}

func (g *Game) isGameReady() bool {
	return g.Player1 != nil && g.Player2 != nil
}

func (g *Game) exit() {
	g.Done <- struct{}{}
}

func (g *Game) secondsRemaining() int64 {
	if g.StartedAt == nil {
		return gameDurationSeconds
	}
	return gameDurationSeconds - (time.Now().Unix() - g.StartedAt.Unix())
}

func (g *Game) GameStateMessage() MessageGameState {
	return MessageGameState{
		Type: MessageTypeGameState,
		State: GameState{
			Width:            g.Width,
			Height:           g.Height,
			Ball:             g.Ball,
			Collision:        g.Collision,
			SecondsRemaining: g.secondsRemaining(),
			Streak:           g.Streak,
		},
	}
}

func (g *Game) ballHitsBottom() bool {
	return g.Ball.Y+g.Ball.Radius >= g.Height
}

func (g *Game) ballHitsTop() bool {
	return g.Ball.Y-g.Ball.Radius <= 0
}

func (g *Game) ballHitsRight() bool {
	return g.Ball.X+g.Ball.Radius >= g.Width
}

func (g *Game) ballHitsLeft() bool {
	return g.Ball.X-g.Ball.Radius <= 0
}

func (g *Game) handleLeftPaddleCollision() bool {
	p := g.Player1.Paddle
	if p == nil {
		return false
	}
	if p.X+p.Width >= g.Ball.X-g.Ball.Radius {
		if g.Ball.Y <= p.Y+p.Height && g.Ball.Y >= p.Y {
			g.Ball.Dx = -g.Ball.Dx
			g.Player1.Score++
			return true
		}
	}
	return false
}

func (g *Game) handleRightPaddleCollision() bool {
	p := g.Player2.Paddle
	if p == nil {
		return false
	}
	if p.X <= g.Ball.X+g.Ball.Radius {
		if g.Ball.Y <= p.Y+p.Height && g.Ball.Y >= p.Y {
			g.Ball.Dx = -g.Ball.Dx
			g.Player2.Score++
			return true
		}
	}
	return false
}

func (g *Game) update() {
	g.Ball.update()
	g.Player1.Paddle.update(g)
	g.Player2.Paddle.update(g)

	g.Collision = false
	leftCollision := g.handleLeftPaddleCollision()
	rightCollision := g.handleRightPaddleCollision()
	g.Collision = leftCollision || rightCollision
	if g.Collision {
		g.Streak++
	}

	if g.Streak >= hitStreak && math.Abs(float64(g.Ball.Dx)) < maxBallDx {
		g.Ball.increaseSpeed()
	}

	if g.ballHitsBottom() || g.ballHitsTop() {
		g.Ball.Dy = -g.Ball.Dy
	}

	if g.ballHitsRight() || g.ballHitsLeft() {
		if g.ballHitsLeft() {
			g.Player2.Score += 10
		} else if g.ballHitsRight() {
			g.Player1.Score += 10
		}
		g.Ball.Dx = -g.Ball.Dx
		g.Streak = 0
	}
}

func (g *Game) broadcast(msg []byte) {
	go g.Player1.pushBytes(msg)
	go g.Player2.pushBytes(msg)
}

func (g *Game) broadcastGameState() {
	p1Msg := g.GameStateMessage()
	p1Msg.State.Me = g.Player1
	p1Msg.State.Opponent = g.Player2
	go g.Player1.push(p1Msg)

	p2Msg := g.GameStateMessage()
	p2Msg.State.Me = g.Player2
	p2Msg.State.Opponent = g.Player1
	go g.Player2.push(p2Msg)
}

func (g *Game) gameStartCountdown() error {
	log.Printf("game %s starting countdown...\n", g.Id)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var i = 5
	for range ticker.C {
		data, err := json.Marshal(MessageGameStartCountdown{
			Type:    MessageTypeGameStartCountdown,
			Counter: i,
		})
		if err != nil {
			return fmt.Errorf("failed to marshal game start countdown: %s", err)
		}
		g.broadcast(data)
		i--
		if i < 0 {
			break
		}
	}
	return nil
}

func (g *Game) run() {
	log.Printf("game %s running...\n", g.Id)
	if g.StartedAt != nil {
		return
	}
	g.broadcastGameState()
	if err := g.gameStartCountdown(); err != nil {
		log.Printf("failed to start game %s countdown: %v\n", g.Id, err)
	}
	start := time.Now()
	g.StartedAt = &start
	ticker := time.NewTicker(time.Second / framesPerSecond)
	defer ticker.Stop()
	gameOver := false
	for !gameOver {
		select {
		case <-g.Player1.exit:
			log.Printf("player1 %s exited game %s\n", g.Player1.Id, g.Id)
			g.Player2.push(MessageOpponentDisconnected{
				Type: MessageTypeOpponentDisconnected,
			})
			gameOver = true
		case <-g.Player2.exit:
			log.Printf("player2 %s exited game %s\n", g.Player2.Id, g.Id)
			g.Player1.push(MessageOpponentDisconnected{
				Type: MessageTypeOpponentDisconnected,
			})
			gameOver = true
		case t := <-ticker.C:
			g.update()
			g.broadcastGameState()
			if t.Unix()-g.StartedAt.Unix() == gameDurationSeconds {
				gameOver = true
			}
		}
	}
	log.Printf("game %s has ended\n", g.Id)
	now := time.Now()
	g.EndedAt = &now
}
