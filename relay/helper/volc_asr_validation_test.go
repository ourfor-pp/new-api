package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateDoubaoASRFileBoundaries(t *testing.T) {
	tests := []struct {
		name       string
		filename   string
		size       int64
		duration   float64
		wantFormat string
		wantError  string
	}{
		{name: "WAV 上限可用", filename: "audio.wav", size: 100 * 1024 * 1024, duration: 7200, wantFormat: "wav"},
		{name: "MP3 可用", filename: "audio.mp3", size: 1024, duration: 1, wantFormat: "mp3"},
		{name: "Opus 映射为 OGG", filename: "audio.opus", size: 1024, duration: 1, wantFormat: "ogg"},
		{name: "超过 100MB", filename: "audio.wav", size: 100*1024*1024 + 1, duration: 1, wantError: "100MB"},
		{name: "超过两小时", filename: "audio.wav", size: 1024, duration: 7200.001, wantError: "2 hours"},
		{name: "不支持格式", filename: "audio.flac", size: 1024, duration: 1, wantError: "WAV, MP3, OGG, or Opus"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			format, err := validateDoubaoASRFile(test.filename, test.size, test.duration)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.wantFormat, format)
		})
	}
}
