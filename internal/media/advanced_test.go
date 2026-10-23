package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"testing"
)

func fixture(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestAdvancedContainers(t *testing.T) {
	video := fixture(t, "video.mp4")
	got, err := InspectVideo(video)
	if err != nil || got.Width != 32 || got.Height != 32 || got.Seconds != 1 {
		t.Fatalf("video metadata: %+v %v", got, err)
	}
	if _, err := InspectVideo(fixture(t, "video-audio.mp4")); err != nil {
		t.Fatalf("video with AAC audio: %v", err)
	}
	for _, name := range []string{"sticker.webp", "animated.webp"} {
		data := fixture(t, name)
		got, err := InspectSticker(data)
		if err != nil || got.Width != 512 || got.Height != 512 || got.Animated != (name == "animated.webp") {
			t.Fatalf("%s metadata: %+v %v", name, got, err)
		}
	}
	for _, tc := range []struct {
		name    string
		data    []byte
		inspect func([]byte) (Info, error)
	}{
		{"truncated video", video[:len(video)-1], InspectVideo},
		{"wrong codec", bytes.ReplaceAll(video, []byte("avc1"), []byte("hvc1")), InspectVideo},
		{"wrong audio codec", bytes.ReplaceAll(fixture(t, "video-audio.mp4"), []byte("mp4a"), []byte("ac-3")), InspectVideo},
		{"missing media", bytes.ReplaceAll(video, []byte("mdat"), []byte("free")), InspectVideo},
		{"overflow box", append([]byte{0, 0, 0, 1, 'm', 'o', 'o', 'v'}, bytes.Repeat([]byte{255}, 8)...), InspectVideo},
		{"truncated webp", fixture(t, "sticker.webp")[:20], InspectSticker},
		{"wrong canvas", bytes.ReplaceAll(fixture(t, "animated.webp"), []byte{0xff, 1, 0, 0xff, 1, 0}, []byte{0, 0, 0, 0, 0, 0}), InspectSticker},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.inspect(tc.data); err == nil {
				t.Fatal("accepted invalid container")
			}
		})
	}
}

func TestStickerLimitsAndAnimation(t *testing.T) {
	static := fixture(t, "sticker.webp")
	// Unknown RIFF chunks are legal, but must count toward the upload limit.
	chunk := make([]byte, (100<<10)+8)
	copy(chunk, "EXIF")
	binary.LittleEndian.PutUint32(chunk[4:8], 100<<10)
	large := append(bytes.Clone(static), chunk...)
	binary.LittleEndian.PutUint32(large[4:8], uint32(len(large)-8))
	if _, err := InspectSticker(large); !errors.Is(err, ErrStickerTooLarge) {
		t.Fatalf("size: %v", err)
	}
	animated := fixture(t, "animated.webp")
	for _, ms := range []uint32{0, 10001} {
		bad := bytes.Clone(animated)
		index := bytes.Index(bad, []byte("ANMF"))
		if index < 0 {
			t.Fatal("fixture lacks frame")
		}
		offset := index + 8 + 12
		bad[offset], bad[offset+1], bad[offset+2] = byte(ms), byte(ms>>8), byte(ms>>16)
		if _, err := InspectSticker(bad); err == nil {
			t.Fatal("accepted invalid frame duration")
		}
	}
}

func FuzzAdvancedContainers(f *testing.F) {
	for _, name := range []string{"video.mp4", "sticker.webp", "animated.webp"} {
		f.Add(fixture(f, name))
	}
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxBytes {
			return
		}
		InspectVideo(data)
		InspectSticker(data)
	})
}
