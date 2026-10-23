package media

import (
	"encoding/binary"
	"errors"
	"fmt"
)

var ErrStickerTooLarge = errors.New("sticker exceeds 100 KiB static or 500 KiB animated limit")

// Info is bounded container metadata. Inspection does not decode or transcode.
type Info struct {
	Width, Height, Seconds uint32
	Animated               bool
}

type box struct {
	name string
	data []byte
}

// boxes checks every length before slicing, including extended ISO box sizes.
func boxes(data []byte) ([]box, error) {
	var out []box
	for len(data) > 0 {
		if len(data) < 8 || len(out) >= 4096 {
			return nil, errors.New("invalid MP4 boxes")
		}
		size, header := uint64(binary.BigEndian.Uint32(data)), uint64(8)
		if size == 1 {
			if len(data) < 16 {
				return nil, errors.New("truncated MP4 box")
			}
			size, header = binary.BigEndian.Uint64(data[8:]), 16
		} else if size == 0 {
			size = uint64(len(data))
		}
		if size < header || size > uint64(len(data)) {
			return nil, errors.New("invalid MP4 box size")
		}
		out = append(out, box{string(data[4:8]), data[header:size]})
		data = data[size:]
	}
	return out, nil
}

func child(data []byte, name string) ([]byte, error) {
	items, err := boxes(data)
	if err != nil {
		return nil, err
	}
	var found []byte
	for _, b := range items {
		if b.name == name {
			if found != nil {
				return nil, fmt.Errorf("duplicate MP4 %s", name)
			}
			found = b.data
		}
	}
	if found == nil {
		return nil, fmt.Errorf("missing MP4 %s", name)
	}
	return found, nil
}

// InspectVideo accepts MP4 with one H.264 video track and optional AAC audio.
func InspectVideo(data []byte) (Info, error) {
	var info Info
	ftyp, err := child(data, "ftyp")
	if err != nil || len(ftyp) < 8 || len(ftyp)%4 != 0 {
		return info, errors.New("invalid MP4 ftyp")
	}
	mdat, err := child(data, "mdat")
	if err != nil || len(mdat) == 0 {
		return info, errors.New("MP4 requires nonempty mdat")
	}
	moov, err := child(data, "moov")
	if err != nil {
		return info, err
	}
	tracks, err := boxes(moov)
	if err != nil {
		return info, err
	}
	videos, audios := 0, 0
	for _, track := range tracks {
		if track.name != "trak" {
			continue
		}
		mdia, err := child(track.data, "mdia")
		if err != nil {
			return info, err
		}
		hdlr, err := child(mdia, "hdlr")
		if err != nil || len(hdlr) < 12 {
			return info, errors.New("invalid MP4 handler")
		}
		handler := string(hdlr[8:12])
		if handler != "vide" && handler != "soun" {
			return info, errors.New("MP4 supports only video and audio tracks")
		}
		minf, err := child(mdia, "minf")
		if err != nil {
			return info, err
		}
		stbl, err := child(minf, "stbl")
		if err != nil {
			return info, err
		}
		stsd, err := child(stbl, "stsd")
		if err != nil || len(stsd) < 8 || binary.BigEndian.Uint32(stsd[4:8]) != 1 {
			return info, errors.New("MP4 requires one sample description per track")
		}
		entries, err := boxes(stsd[8:])
		if err != nil || len(entries) != 1 {
			return info, errors.New("invalid MP4 sample description")
		}
		entry := entries[0]
		if handler == "soun" {
			audios++
			if audios > 1 || entry.name != "mp4a" || len(entry.data) < 28 {
				return info, errors.New("MP4 audio must be a single AAC track")
			}
			continue
		}
		videos++
		if videos > 1 || entry.name != "avc1" || len(entry.data) < 78 {
			return info, errors.New("MP4 video must be a single H.264 avc1 track")
		}
		avcc, err := child(entry.data[78:], "avcC")
		if err != nil || len(avcc) < 7 || avcc[0] != 1 {
			return info, errors.New("invalid H.264 configuration")
		}
		info.Width = uint32(binary.BigEndian.Uint16(entry.data[24:26]))
		info.Height = uint32(binary.BigEndian.Uint16(entry.data[26:28]))
		if info.Width == 0 || info.Height == 0 || info.Width > 4096 || info.Height > 4096 {
			return info, errors.New("video dimensions must be between 1 and 4096")
		}
		mdhd, err := child(mdia, "mdhd")
		if err != nil || len(mdhd) < 20 {
			return info, errors.New("invalid MP4 duration")
		}
		var scale, duration uint64
		switch mdhd[0] {
		case 0:
			scale, duration = uint64(binary.BigEndian.Uint32(mdhd[12:16])), uint64(binary.BigEndian.Uint32(mdhd[16:20]))
		case 1:
			if len(mdhd) < 32 {
				return info, errors.New("invalid MP4 duration")
			}
			scale, duration = uint64(binary.BigEndian.Uint32(mdhd[20:24])), binary.BigEndian.Uint64(mdhd[24:32])
		default:
			return info, errors.New("unsupported MP4 duration version")
		}
		if scale == 0 || duration == 0 || duration > scale*3600 {
			return info, errors.New("video duration must be positive and at most one hour")
		}
		info.Seconds = uint32((duration + scale - 1) / scale)
	}
	if videos != 1 {
		return info, errors.New("MP4 requires one video track")
	}
	return info, nil
}

func le24(data []byte) uint32 { return uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 }

func webpChunks(data []byte) ([]box, error) {
	var out []box
	for len(data) > 0 {
		if len(data) < 8 || len(out) >= 4096 {
			return nil, errors.New("invalid WebP chunks")
		}
		n := uint64(binary.LittleEndian.Uint32(data[4:8]))
		padded := n + n%2
		if padded > uint64(len(data)-8) {
			return nil, errors.New("truncated WebP chunk")
		}
		out = append(out, box{string(data[:4]), data[8 : 8+n]})
		data = data[8+padded:]
	}
	return out, nil
}

func webpDimensions(b box) (uint32, uint32, error) {
	p := b.data
	switch b.name {
	case "VP8 ":
		if len(p) >= 10 && p[0]&1 == 0 && string(p[3:6]) == "\x9d\x01\x2a" {
			return uint32(binary.LittleEndian.Uint16(p[6:8]) & 0x3fff), uint32(binary.LittleEndian.Uint16(p[8:10]) & 0x3fff), nil
		}
	case "VP8L":
		if len(p) >= 5 && p[0] == 0x2f && p[4]>>5 == 0 {
			v := binary.LittleEndian.Uint32(p[1:5])
			return (v & 0x3fff) + 1, ((v >> 14) & 0x3fff) + 1, nil
		}
	}
	return 0, 0, errors.New("invalid WebP image header")
}

// InspectSticker bounds canvas, frames, duration and file size before upload.
// The compressed pixel stream is not decoded; clients must provide valid WebP.
func InspectSticker(data []byte) (Info, error) {
	var info Info
	if len(data) < 20 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" || uint64(binary.LittleEndian.Uint32(data[4:8]))+8 != uint64(len(data)) {
		return info, errors.New("invalid WebP RIFF container")
	}
	chunks, err := webpChunks(data[12:])
	if err != nil {
		return info, err
	}
	images, frames, animations, extended := 0, 0, 0, false
	var duration uint64
	for i, b := range chunks {
		switch b.name {
		case "VP8X":
			if i != 0 || len(b.data) != 10 || b.data[0]&0xc1 != 0 || b.data[1] != 0 || b.data[2] != 0 || b.data[3] != 0 {
				return info, errors.New("invalid extended WebP header")
			}
			extended, info.Animated = true, b.data[0]&2 != 0
			info.Width, info.Height = le24(b.data[4:7])+1, le24(b.data[7:10])+1
		case "VP8 ", "VP8L":
			images++
			w, h, err := webpDimensions(b)
			if err != nil {
				return info, err
			}
			if extended && (w != info.Width || h != info.Height) {
				return info, errors.New("WebP canvas mismatch")
			}
			info.Width, info.Height = w, h
		case "ANIM":
			animations++
			if !info.Animated || animations != 1 || frames != 0 || len(b.data) != 6 {
				return info, errors.New("invalid WebP animation header")
			}
		case "ANMF":
			frames++
			if !info.Animated || animations != 1 || frames > 120 || len(b.data) < 16 || b.data[15]&0xfc != 0 {
				return info, errors.New("invalid WebP frame")
			}
			x, y := le24(b.data[:3])*2, le24(b.data[3:6])*2
			w, h := le24(b.data[6:9])+1, le24(b.data[9:12])+1
			ms := le24(b.data[12:15])
			duration += uint64(ms)
			if x+w > info.Width || y+h > info.Height || ms < 10 || duration > 10000 {
				return info, errors.New("WebP frame exceeds canvas or duration limits")
			}
			sub, err := webpChunks(b.data[16:])
			if err != nil {
				return info, err
			}
			count := 0
			for _, image := range sub {
				if image.name == "ALPH" {
					continue
				}
				iw, ih, err := webpDimensions(image)
				if err != nil || iw != w || ih != h {
					return info, errors.New("invalid WebP frame image")
				}
				count++
			}
			if count != 1 {
				return info, errors.New("WebP frame requires one image")
			}
		}
	}
	if info.Width != 512 || info.Height != 512 {
		return info, errors.New("stickers require a 512x512 canvas")
	}
	if (info.Animated && (frames == 0 || images != 0)) || (!info.Animated && (images != 1 || frames != 0 || animations != 0)) {
		return info, errors.New("invalid WebP image/animation layout")
	}
	limit := 100 << 10
	if info.Animated {
		limit = 500 << 10
	}
	if len(data) > limit {
		return info, ErrStickerTooLarge
	}
	return info, nil
}
