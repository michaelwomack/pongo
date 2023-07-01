const canvas = document.getElementById("canvas");
const ctx = canvas.getContext("2d");
let raf, lastInputEvent;

const state = {
    ball: null,
    me: null,
    opponent: null,
    secondsRemaining: null,
    streak: 0,
    gameStartCountdown: null,
    opponentDisconnected: false,
};

const bounce = new Audio("./ball-bounce.wav");

// Events
const CLIENT_CONNECTED = 1;
const GAME_STATE = 2;
const PLAYER_INPUT = 3;
const GAME_START_COUNTDOWN = 4;
const OPPONENT_DISCONNECTED = 5;

// Constants
const PADDLE_DY = 6;

class Paddle {
    constructor({ x, y, dx, dy, width, height }) {
        this.x = x;
        this.y = y;
        this.dx = dx;
        this.dy = dy;
        this.width = width;
        this.height = height;
    }

    update({ x, y, dx, dy }) {
        this.x = x;
        this.y = y;
        this.dx = dx;
        this.dy = dy;
    }

    draw() {
        ctx.beginPath();
        ctx.fillStyle = "white";
        ctx.rect(this.x, this.y, this.width, this.height);
        ctx.fill();
    }
}

class Ball {
    constructor({ x, y, dx, dy, radius }) {
        this.x = x;
        this.y = y;
        this.dx = dx;
        this.dy = dy;
        this.color = "#a103fc";
        this.radius = radius;
    }

    update({ x, y, dx, dy }) {
        this.x = x;
        this.y = y;
        this.dx = dx;
        this.dy = dy;
    }

    draw() {
        ctx.beginPath();
        ctx.arc(this.x, this.y, this.radius, 0, Math.PI * 2, true);
        ctx.closePath();
        ctx.fillStyle = this.color;
        ctx.fill();
    }
}

const draw = () => {
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.fillSyle = "black";

    if (state.gameStartCountdown) {
        ctx.font = "72px serif";
        ctx.textAlign = "center";
        ctx.fillStyle = "white";
        ctx.fillText(state.gameStartCountdown, canvas.width/2, canvas.height/2 - 70);

        ctx.font = "24px serif";
        ctx.fillStyle = "white";
        ctx.textAlign = "center";
        ctx.fillText("you are here", state.me.isLeft ? 150: canvas.width - 150, canvas.height / 2);
    }

    if (state.secondsRemaining) {
        const m = Math.floor(state.secondsRemaining / 60);
        const s = state.secondsRemaining % 60;
        const display = String(m).padStart(2, "0") + ":" + String(s).padStart(2, "0");
        ctx.font = "36px serif";
        ctx.textAlign = "center";
        ctx.fillStyle = "white";
        ctx.fillText(display, canvas.width/2, 30);

        ctx.font = "24px serif";
        ctx.textAlign = "center";
        ctx.fillStyle = "white";
        ctx.fillText(`streak: ${state.streak}`, canvas.width/2, 70);
    } else if (state.secondsRemaining === 0) {
        ctx.font = "72px serif";
        ctx.textAlign = "center"
        ctx.fillStyle = "white";
        const text = `${state.me.score > state.opponent.score ? "Winner": "Loser"}!`
        ctx.fillText(text, canvas.width/2, canvas.height/2);
    }

    if (state.opponentDisconnected) {
        ctx.font = "72px serif";
        ctx.textAlign = "center";
        ctx.fillStyle = "white";
        ctx.fillText("Opponent Disconnected", canvas.width / 2, canvas.height / 2 - 70);
    }

    if (state.ball) {
        state.ball.draw();
    }

    if (state.me?.paddle) {
        ctx.font = "24px serif";
        ctx.fillStyle = "white";
        ctx.textAlign = "center";
        ctx.fillText(state.me.score, state.me.isLeft ? 50: canvas.width - 50, 30);
        state.me.paddle.draw();
    }

    if (state.opponent?.paddle) {
        ctx.font = "24px serif";
        ctx.fillStyle = "white";
        ctx.textAlign = "center";
        ctx.fillText(state.opponent.score, state.opponent.isLeft ? 50: canvas.width - 50, 30);
        state.opponent.paddle.draw();
    }
};

document.addEventListener("keydown", function (event) {
    if (!state.me?.paddle) {
        return;
    }
    switch (event.code) {
        case "ArrowDown": // down
            state.me.paddle.dy = PADDLE_DY;
            break;
        case "ArrowUp": // up
            state.me.paddle.dy = -PADDLE_DY;
            break;
    }
    // We can determine paddle movement on the server, we just need to know when the
    // direction changes. There's no need to continue to send the paddle.
    if (lastInputEvent !== "keydown") {
        socket.send(JSON.stringify({ type: PLAYER_INPUT, paddle: state.me.paddle }));
        lastInputEvent = "keydown";
    }
});

document.addEventListener("keyup", function (event) {
    if (!state.me?.paddle) {
        return;
    }
    state.me.paddle.dy = 0;
    socket.send(JSON.stringify({ type: PLAYER_INPUT, paddle: state.me.paddle }));
    lastInputEvent = "keyup";
});

const params = new URLSearchParams(window.location.search);
const scheme = window.location.host.startsWith("localhost") ? "ws" : "wss";
const socket = new WebSocket(`${scheme}://${window.location.host}/ws?code=${params.get("code")}`);

socket.addEventListener("open", function(event){
    console.log("socket opened: ", event);
});

socket.addEventListener("error", function(event){
    console.log("socket error: ", event);
});

socket.addEventListener("close", function(event){
    console.log("socket close: ", event);
});

socket.addEventListener("message",  function(event){
    const data = JSON.parse(event.data);
    switch (data.type) {
        case CLIENT_CONNECTED:
            state.me = {
                id: data.me.id,
                score: data.me.score,
                isLeft: data.me.isLeft,
                paddle: new Paddle(data.me.paddle),
            };
            console.log(`${data.me.id} connected...`);
            break;
        case GAME_START_COUNTDOWN:
            state.gameStartCountdown = data.counter;
            break;
        case OPPONENT_DISCONNECTED:
            state.opponentDisconnected = true;
            setInterval(() => {
                window.location.search = "";
                window.location.pathname = "/";
            }, 5000);
            break;
        case GAME_STATE:
            const { collision, secondsRemaining, me, opponent, ball, streak } = data.state;
            if (collision) {
                bounce.play();
            }

            state.streak = streak;
            state.secondsRemaining = secondsRemaining;

            if (state.ball === null) {
                state.ball = new Ball(ball);
            }
            state.ball.update(ball);

            if (state.opponent === null) {
                state.opponent = {
                    id: opponent.id,
                    score: opponent.score,
                    isLeft: opponent.isLeft,
                    paddle: new Paddle(opponent.paddle),
                }
            }
            state.opponent.score = opponent.score;
            state.opponent.paddle.update(opponent.paddle || {});

            state.me.score = me.score;
            state.me.paddle.update(me.paddle || {});
            break;
    }
    raf = window.requestAnimationFrame(draw);
});

