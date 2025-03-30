package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	"github.com/samber/lo"
	"golang.org/x/sync/errgroup"
)

var (
	reNewGame       = regexp.MustCompile(`^Gametype\s+'([^']+)'\s+initialized`)
	reCarret        = regexp.MustCompile(`\^(\d)`)
	reConnection    = regexp.MustCompile(`^(.+)\sconnected\sfrom\s([\d\.]+):\d+`)
	reEnter         = regexp.MustCompile(`^(.+)\sentered the game`)
	reJoinTeam      = regexp.MustCompile(`^(.+)\sjoined the ([^\s]+) team.`)
	reSpeak         = regexp.MustCompile(`^(.+):\s(.+)`)
	reDisconnection = regexp.MustCompile(`^(.+)\sdisconnected`)

	// all these regexp are for frags
	// - Instagib frag (example:  "%APPDATA%^7 was instagibbed by Sid^7's instabeam")
	reFragInstagib = regexp.MustCompile(`^(.+)\swas instagibbed by (.+)'s instabeam`)
	// - Rocket launcher frag (example: "P.E.#1^7 ate Monada^7's rocket")
	reFragRocketLauncher = regexp.MustCompile(`^(.+)\sate (.+)'s rocket`)
	// - P.E.#1^7 almost dodged Monada^7's rocket
	reFragRockerLauncher2 = regexp.MustCompile(`^(.+)\salmost dodged (.+)'s rocket`)
	// - Riotgun frag (example: "P.E.#1^7 was shred by Monada^7's riotgun")
	reFragRiotgun = regexp.MustCompile(`^(.+)\swas shred by (.+)'s riotgun`)
	// - Lasergun frag (example: "P.E.#1^7 was cut by Monada^7's lasergun")
	reLasergun = regexp.MustCompile(`^(.+)\swas cut by (.+)'s lasergun`)
	// - Plasmagun frag (example: "P.E.#1^7 was melted by Monada^7's plasmagun")
	rePlasmaGun = regexp.MustCompile(`^(.+)\swas melted by (.+)'s plasmagun`)
	// - GrenadeLauncher frag (example: "P.E.#1^7 didn't see Monada^7's grenade")
	reGrenadeLauncher = regexp.MustCompile(`^(.+)\sdidn't see (.+)'s grenade`)
	// - Grenade Launcher frag (example: "P.E.#1^7 was popped by Monada^7's grenade")
	reGrenadeLauncher2 = regexp.MustCompile(`^(.+)\swas popped by (.+)'s grenade`)
	// - Self frag (example: "P.E.#1 ^7died"
	reSelfFrag = regexp.MustCompile(`^(.+)\s\^7died`)

	// since we try to parse what people say and this is very close to system message we have to create a blacklist
	// of player names (so we detect them as system messages)
	// sadly anybody with this name will not be detected as a player when they speak
	playerNameBlacklist = map[string]bool{
		"G_LoadGameScript":        true,
		"       ":                 true,
		"Opening UDP/IP socket":   true,
		"Opening UDP/IPv6 socket": true,
		"SpawnServer":             true,
	}
)

func main() {
	path := flag.String("p", "", "Path to the file to write on top of stdout (like tee but unbuffered)")
	flag.Parse()
	if *path == "" {
		fmt.Println("Error: File path is required. Use -p <path>")
		os.Exit(1)
	}

	writer, err := NewSplitWriter(*path)
	if err != nil {
		fmt.Println("Error opening file:", err)
		os.Exit(1)
	}
	defer func() {
		if err := writer.Close(); err != nil {
			fmt.Println("Error closing file:", err)
		}
	}()

	logger := slog.New(slog.NewJSONHandler(writer, nil))
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// game stores the latest known game data
	// when the command is ran after a game already started, the game is in a bad state
	game := NewGame("")

	scanner := bufio.NewScanner(os.Stdin)
	for Scan(ctx, scanner) {
		t := convertANSIToWarsow(strings.TrimSuffix(scanner.Text(), ansiReset))

		level := slog.LevelInfo
		attrs := []slog.Attr{}
		if victim, killer, weapon := parseFrag(t); killer != "" {
			// this is a frag
			// we need to sanitize the player name
			killer = sanitizePlayer(killer)
			victim = sanitizePlayer(victim)
			weapon = strings.TrimSpace(weapon)

			victimPlayer := game.AddPlayer(victim, "")
			killerPlayer := game.AddPlayer(killer, "")
			killerPlayer.Frag(victim, weapon)

			attrs = append(attrs, killerPlayer.Slog("killer"))
			attrs = append(attrs, victimPlayer.Slog("victim"))
			attrs = append(attrs, slog.String("weapon", weapon))
		} else if strings.Contains(t, "All players are ready. Match starting!") {
			game.Start()
		} else if match := reEnter.FindStringSubmatch(t); len(match) > 0 {
			player := game.AddPlayer(sanitizePlayer(match[1]), "")
			attrs = append(attrs, player.Slog("player"))
		} else if match := reConnection.FindStringSubmatch(t); len(match) > 0 {
			player := game.AddPlayer(sanitizePlayer(match[1]), match[2])
			attrs = append(attrs, player.Slog("player"))
		} else if match := reJoinTeam.FindStringSubmatch(t); len(match) > 0 {
			player := game.AddPlayer(sanitizePlayer(match[1]), "")
			attrs = append(attrs, player.Slog("player"))
		} else if match := reDisconnection.FindStringSubmatch(t); len(match) > 0 {
			player := game.AddPlayer(sanitizePlayer(match[1]), "")
			player.Disconnect()
			attrs = append(attrs, player.Slog("player"))
		} else if strings.Contains(t, "-------------------------------------") {
			game.End()
			if game.IsFullGame() {
				attrs = append(
					attrs,
					slog.String("game_type", game.GameType),
					slog.Bool("full_game", true),
				)
				fullBot := true
				scores := make([]slog.Attr, 0, len(game.Players()))
				players := lo.Map(game.Players(), func(p *Player, _ int) slog.Attr {
					scores = append(
						scores,
						slog.Attr{
							Key:   p.Name,
							Value: slog.GroupValue(p.SlogScores()...),
						},
					)
					fullBot = fullBot && p.IsBot()
					return p.Slog(p.Name)
				})
				attrs = append(
					attrs,
					slog.Attr{
						Key:   "players",
						Value: slog.GroupValue(players...),
					},
				)
				attrs = append(
					attrs,
					slog.Attr{
						Key:   "scores",
						Value: slog.GroupValue(scores...),
					},
				)
				attrs = append(attrs, slog.Bool("full_bot", fullBot))
				attrs = append(attrs, slog.Time("start_at", game.startAt))
				if !fullBot {
					level = slog.LevelWarn
				}
			}
		} else if match := reNewGame.FindStringSubmatch(t); len(match) > 0 {
			gameTypeName := match[1]
			game = NewGame(gameTypeName)

			attrs = append(attrs, slog.String("game_type", game.GameType))
		} else if match := reSpeak.FindStringSubmatch(t); len(match) > 0 && !playerNameBlacklist[match[1]] {
			player := game.AddPlayer(sanitizePlayer(match[1]), "")
			attrs = append(attrs, player.Slog("player"))
			attrs = append(attrs, slog.String("text", match[2]))
		}
		slog.LogAttrs(ctx, level, t, attrs...)
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "Error reading from stdin:", err)
	}
}

var (
	ErrEOF = fmt.Errorf("EOF")
)

func Scan(ctx context.Context, s *bufio.Scanner) bool {
	select {
	case <-ctx.Done():
		return false
	default:
		return s.Scan()
	}
}

var ansiReset = "\u001B[0m"
var ansiToWarsow = map[string]string{
	"\u001B[30m":       "^0", // Black
	"\u001B[31m":       "^1", // Red
	"\u001B[32m":       "^2", // Green
	"\u001B[33m":       "^3", // Yellow
	"\u001B[34m":       "^4", // Blue
	"\u001B[36m":       "^5", // Cyan
	"\u001B[35m":       "^6", // Purple
	"\u001B[37m":       "^7", // White
	"\u001B[38;5;208m": "^8", // Orange (approximation)
	"\u001B[90m":       "^9", // Gray
	"\u001B[0m":        "^7", // Reset (white)
}

var ansiRegex = regexp.MustCompile(`\x1B\[[0-9;]*m`)

func convertANSIToWarsow(input string) string {
	return ansiRegex.ReplaceAllStringFunc(input, func(match string) string {
		if warsowCode, exists := ansiToWarsow[match]; exists {
			return warsowCode
		}
		return "" // Remove unknown ANSI codes
	})
}

// sanitizePlayer cleans the player name by removing unwanted characters
// ^4Su^7ta^1t^7 becomes ^4Su^7ta^1t
func sanitizePlayer(name string) string {
	trimmed := strings.TrimSpace(name)

	i := strings.LastIndex(trimmed, "^")
	if i == -1 || i < len(trimmed)-2 {
		return trimmed
	}
	return trimmed[:i]
}

func playerFlat(name string) string {
	return reCarret.ReplaceAllString(name, "")
}

func parseFrag(text string) (string, string, string) {
	// %APPDATA%^7 was instagibbed by Sid^7's instabeam
	if match := reFragInstagib.FindStringSubmatch(text); len(match) >= 3 {
		victim := match[1]
		killer := match[2]
		return victim, killer, "instagib"
	}
	// P.E.#1^7 ate Monada^7's rocket
	if match := reFragRocketLauncher.FindStringSubmatch(text); len(match) >= 3 {
		victim := match[1]
		killer := match[2]
		return victim, killer, "rocket"
	}
	// P.E.#1^7 almost dodged Monada^7's rocket
	if match := reFragRockerLauncher2.FindStringSubmatch(text); len(match) >= 3 {
		victim := match[1]
		killer := match[2]
		return victim, killer, "rocket"
	}
	// P.E.#1^7 was shred by Monada^7's riotgun
	if match := reFragRiotgun.FindStringSubmatch(text); len(match) >= 3 {
		victim := match[1]
		killer := match[2]
		return victim, killer, "riotgun"
	}
	// P.E.#1^7 was cut by Monada^7's lasergun
	if match := reLasergun.FindStringSubmatch(text); len(match) >= 3 {
		victim := match[1]
		killer := match[2]
		return victim, killer, "lasergun"
	}
	// P.E.#1^7 was melted by Monada^7's plasmagun
	if match := rePlasmaGun.FindStringSubmatch(text); len(match) >= 3 {
		victim := match[1]
		killer := match[2]
		return victim, killer, "plasmagun"
	}
	// P.E.#1^7 didn't see Monada^7's grenade
	if match := reGrenadeLauncher.FindStringSubmatch(text); len(match) >= 3 {
		victim := match[1]
		killer := match[2]
		return victim, killer, "grenade"
	}
	// P.E.#1^7 was popped by Monada^7's grenade
	if match := reGrenadeLauncher2.FindStringSubmatch(text); len(match) >= 3 {
		victim := match[1]
		killer := match[2]
		return victim, killer, "grenade"
	}
	// P.E.#1^7 ^7died
	if match := reSelfFrag.FindStringSubmatch(text); len(match) >= 2 {
		victim := match[1]
		killer := match[1]
		return victim, killer, "self"
	}

	return "", "", ""
}

type SplitWriter struct {
	stdout io.Writer
	file   *os.File
}

// NewSplitWriter creates a new SplitWriter.
func NewSplitWriter(filePath string) (*SplitWriter, error) {
	// Open the file for writing, create if not exists, append if exists.
	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	return &SplitWriter{
		stdout: os.Stdout, // Writes to standard output
		file:   file,      // Writes to the file
	}, nil
}

func (w *SplitWriter) Write(p []byte) (n int, err error) {
	eg := &errgroup.Group{}
	eg.Go(func() error {
		n, err = w.stdout.Write(p)
		return err
	})
	eg.Go(func() error {
		_, err = w.file.Write(p)
		return err
	})
	return n, eg.Wait()
}

func (w *SplitWriter) Close() error {
	return w.file.Close()
}
