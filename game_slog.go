package main

import (
	"log/slog"
)

func (p *Player) Slog(prefix string) slog.Attr {
	return slog.Group(
		prefix,
		slog.String("name", p.Name),
		slog.String("text_name", p.TextName),
		slog.String("ip", p.IP),
		slog.Bool("connected", p.connected),
		slog.Bool("is_bot", p.IsBot()),
	)
}

func (p *Player) SlogScores() []slog.Attr {
	scores := make([]slog.Attr, 0, len(p.Scores))
	total := 0
	for k, v := range p.Scores {
		total += v
		if k == p.Name {
			scores = append(scores, slog.Int("@@suicide@@", v))
		} else {
			scores = append(scores, slog.Int(k, v))
		}
	}
	scores = append(scores, slog.Int("@@total@@", total))
	return scores
}
