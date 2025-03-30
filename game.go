package main

import (
	"strconv"
	"strings"
	"time"

	"github.com/samber/lo"
)

type Game struct {
	// false if the game is not registered from the beginning
	// it happens when we bound the logs of an already started game/server
	hasStarted bool
	hasEnded   bool
	GameType   string
	startAt    time.Time
	players    map[string]*Player
}

func NewGame(gameType string) *Game {
	return &Game{
		players:    make(map[string]*Player),
		hasStarted: false,
		GameType:   gameType,
	}
}

func (g *Game) Players() []*Player {
	return lo.Values(g.players)
}

func (g *Game) Start() {
	g.hasStarted = true
	g.startAt = time.Now()
}

func (g *Game) End() {
	g.hasEnded = true
}

func (g *Game) AddPlayer(name, ip string) *Player {
	player, ok := g.players[name]
	if !ok {
		player = NewPlayer(name)
		g.players[name] = player
	}
	player.connected = true
	if len(ip) > 0 {
		player.IP = ip
	}
	return player
}

func (g *Game) IsClean() bool {
	return g.hasStarted && g.GameType != ""
}

func (g *Game) IsFullGame() bool {
	return g.IsClean() && g.hasEnded
}

func (g *Game) String() string {
	sb := strings.Builder{}
	sb.WriteString("Game type: ")
	sb.WriteString(g.GameType)
	sb.WriteString("\n")
	sb.WriteString("Players:\n")
	for _, p := range g.players {
		if p.IsBot() {
			continue
		}
		sb.WriteString("\t- ")
		sb.WriteString(p.String())
		sb.WriteString("\n")
	}
	sb.WriteString("Bots:\n")
	for _, p := range g.players {
		if !p.IsBot() {
			continue
		}
		sb.WriteString("\t- ")
		sb.WriteString(p.String())
		sb.WriteString("\n")
	}
	return sb.String()
}

type Player struct {
	Name      string
	TextName  string
	IP        string
	connected bool
	// playerName -> score
	Scores map[string]int
}

func NewPlayer(name string) *Player {
	return &Player{
		Name:     name,
		TextName: playerFlat(name),
		Scores:   make(map[string]int),
	}
}

func (p *Player) Disconnect() {
	p.connected = false
}

func (p *Player) IsBot() bool {
	return len(p.IP) == 0
}

func (p *Player) String() string {
	// name + ip if exists + some of scores if any
	sb := strings.Builder{}
	sb.WriteString(p.Name)
	if len(p.TextName) > 0 {
		sb.WriteString(" (")
		sb.WriteString(p.TextName)
		sb.WriteString(")")
	}
	if len(p.IP) > 0 {
		sb.WriteString(" [")
		sb.WriteString(p.IP)
		sb.WriteString("]")
	}
	if len(p.Scores) > 0 {
		total := 0
		frags := 0
		for name, v := range p.Scores {
			total += v
			if name != p.Name {
				frags += v
			}
		}
		sb.WriteString(" scores ")
		sb.WriteString(strconv.Itoa(total))
		sb.WriteString(" with ")
		sb.WriteString(strconv.Itoa(frags))
		sb.WriteString(" frag(s)!")
		suicide := frags - total
		if suicide > 0 {
			sb.WriteString(" ... and ")
			sb.WriteString(strconv.Itoa(suicide))
			sb.WriteString(" self kill(s) ...")
		}
	}
	return sb.String()
}

// TODO: second argument is the weapon
func (p *Player) Frag(name string, _ string) {
	if name == p.Name {
		p.Scores[name]--
	} else {
		p.Scores[name]++
	}
}
