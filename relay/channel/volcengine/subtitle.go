package volcengine

import (
	"fmt"
	"strings"
)

type volcSubtitleCue struct {
	StartMS int64
	EndMS   int64
	Text    string
}

func formatVolcSRT(cues []volcSubtitleCue) string {
	var builder strings.Builder
	for index, cue := range cues {
		_, _ = fmt.Fprintf(
			&builder,
			"%d\n%s --> %s\n%s\n\n",
			index+1,
			formatVolcSubtitleTimestamp(cue.StartMS, ','),
			formatVolcSubtitleTimestamp(cue.EndMS, ','),
			cue.Text,
		)
	}
	return builder.String()
}

func formatVolcVTT(cues []volcSubtitleCue) string {
	var builder strings.Builder
	builder.WriteString("WEBVTT\n\n")
	for _, cue := range cues {
		_, _ = fmt.Fprintf(
			&builder,
			"%s --> %s\n%s\n\n",
			formatVolcSubtitleTimestamp(cue.StartMS, '.'),
			formatVolcSubtitleTimestamp(cue.EndMS, '.'),
			cue.Text,
		)
	}
	return builder.String()
}

func formatVolcSubtitleTimestamp(milliseconds int64, separator rune) string {
	hours := milliseconds / 3_600_000
	milliseconds %= 3_600_000
	minutes := milliseconds / 60_000
	milliseconds %= 60_000
	seconds := milliseconds / 1_000
	milliseconds %= 1_000
	return fmt.Sprintf("%02d:%02d:%02d%c%03d", hours, minutes, seconds, separator, milliseconds)
}
