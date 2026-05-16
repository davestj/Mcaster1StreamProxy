// Package icy implements the ICY 1.x / ICY2 v2.2 metadata strip protocol.
//
// Icecast/SHOUTcast streams embed metadata inline every `metaint` bytes:
//
//	[audio bytes (metaint)] [1 byte length L] [L*16 bytes metadata] [audio bytes] ...
//
// The Stripper reads arbitrary chunks from upstream, extracts pure audio,
// and captures StreamTitle changes via a callback.
package icy

import (
	"strings"
)

// State constants for the FSM.
const (
	stateAudio   = 0
	stateMetaLen = 1
	stateMetaBod = 2
)

// TitleCallback is called when StreamTitle changes. Must be goroutine-safe.
type TitleCallback func(title string)

// Stripper implements a 3-state finite state machine that strips inline
// ICY metadata from an audio byte stream. Feed it arbitrary-sized chunks
// via Strip() and it returns only the clean audio bytes.
type Stripper struct {
	metaint    int
	state      int
	audioRem   int
	metaLen    int
	metaBuf    []byte
	lastTitle  string
	onTitle    TitleCallback
}

// NewStripper creates a metadata stripper for the given metaint interval.
// If metaint <= 0, Strip() passes all data through unchanged (no metadata).
func NewStripper(metaint int, onTitle TitleCallback) *Stripper {
	return &Stripper{
		metaint:  metaint,
		state:    stateAudio,
		audioRem: metaint,
		onTitle:  onTitle,
	}
}

// Strip processes a chunk of data from upstream. Returns only the audio
// bytes with all ICY metadata blocks removed. The returned slice may
// alias the input — caller must consume or copy before the next call.
func (s *Stripper) Strip(data []byte) []byte {
	if s.metaint <= 0 {
		return data // passthrough — no inline metadata
	}

	dataLen := len(data)
	// Pre-allocate output at full size (most chunks are mostly audio)
	audio := make([]byte, 0, dataLen)
	pos := 0

	for pos < dataLen {
		switch s.state {
		case stateAudio:
			avail := dataLen - pos
			take := avail
			if take > s.audioRem {
				take = s.audioRem
			}
			if take > 0 {
				audio = append(audio, data[pos:pos+take]...)
				pos += take
				s.audioRem -= take
			}
			if s.audioRem <= 0 {
				s.state = stateMetaLen
			}

		case stateMetaLen:
			s.metaLen = int(data[pos]) * 16
			pos++
			if s.metaLen == 0 {
				// No metadata this cycle
				s.state = stateAudio
				s.audioRem = s.metaint
			} else {
				s.state = stateMetaBod
				s.metaBuf = s.metaBuf[:0] // reset without realloc
			}

		case stateMetaBod:
			need := s.metaLen - len(s.metaBuf)
			avail := dataLen - pos
			take := avail
			if take > need {
				take = need
			}
			s.metaBuf = append(s.metaBuf, data[pos:pos+take]...)
			pos += take

			if len(s.metaBuf) >= s.metaLen {
				// Complete metadata block — parse StreamTitle
				s.parseMetadata()
				s.state = stateAudio
				s.audioRem = s.metaint
			}
		}
	}

	return audio
}

// parseMetadata extracts StreamTitle from the null-padded metadata block.
func (s *Stripper) parseMetadata() {
	// Trim null padding
	meta := strings.TrimRight(string(s.metaBuf), "\x00")
	if meta == "" {
		return
	}

	// Parse StreamTitle='...';
	const prefix = "StreamTitle='"
	idx := strings.Index(meta, prefix)
	if idx < 0 {
		return
	}
	rest := meta[idx+len(prefix):]
	end := strings.Index(rest, "';")
	if end < 0 {
		return
	}
	title := rest[:end]

	if title != s.lastTitle {
		s.lastTitle = title
		if s.onTitle != nil {
			s.onTitle(title)
		}
	}
}

// LastTitle returns the most recently parsed StreamTitle.
func (s *Stripper) LastTitle() string {
	return s.lastTitle
}
