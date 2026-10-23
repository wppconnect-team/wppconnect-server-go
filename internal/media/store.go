package media

import (
	"bytes"
	"container/list"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
)

const CacheEntries = 1024
const CacheTTL = 24 * time.Hour

// Attachment contains only the metadata needed to download a known message.
type Attachment struct {
	Message  whatsmeow.DownloadableMessage
	Mimetype string
	Filename string
	Length   uint64
}

type entry struct {
	id      string
	a       Attachment
	expires time.Time
}

// Store is a per-session bounded FIFO of descriptors, never attachment bytes.
// It is intentionally ephemeral and is discarded when the session closes.
type Store struct {
	mu    sync.Mutex
	items map[string]*list.Element
	order *list.List
	now   func() time.Time
}

func NewStore() *Store {
	return &Store{items: make(map[string]*list.Element), order: list.New(), now: time.Now}
}

func (s *Store) Remember(id string, msg *waE2E.Message) bool {
	a, ok := attachment(msg)
	if !ok || id == "" || len(id) > 256 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old := s.items[id]; old != nil {
		s.order.Remove(old)
	}
	s.items[id] = s.order.PushBack(entry{id: id, a: a, expires: s.now().Add(CacheTTL)})
	for s.order.Len() > CacheEntries {
		oldest := s.order.Front()
		delete(s.items, oldest.Value.(entry).id)
		s.order.Remove(oldest)
	}
	return true
}

func (s *Store) Get(id string) (Attachment, bool) {
	if s == nil {
		return Attachment{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.items[id]
	if e == nil {
		return Attachment{}, false
	}
	v := e.Value.(entry)
	if !s.now().Before(v.expires) {
		delete(s.items, id)
		s.order.Remove(e)
		return Attachment{}, false
	}
	return v.a, true
}

// Unwrap handles protocol wrappers without modifying the event shared by sinks.
func Unwrap(msg *waE2E.Message) *waE2E.Message {
	for depth := 0; msg != nil && depth < 8; depth++ {
		var inner *waE2E.Message
		switch {
		case msg.GetEphemeralMessage().GetMessage() != nil:
			inner = msg.GetEphemeralMessage().GetMessage()
		case msg.GetDocumentWithCaptionMessage().GetMessage() != nil:
			inner = msg.GetDocumentWithCaptionMessage().GetMessage()
		case msg.GetDeviceSentMessage().GetMessage() != nil:
			inner = msg.GetDeviceSentMessage().GetMessage()
		case msg.GetViewOnceMessage().GetMessage() != nil:
			inner = msg.GetViewOnceMessage().GetMessage()
		case msg.GetViewOnceMessageV2().GetMessage() != nil:
			inner = msg.GetViewOnceMessageV2().GetMessage()
		case msg.GetViewOnceMessageV2Extension().GetMessage() != nil:
			inner = msg.GetViewOnceMessageV2Extension().GetMessage()
		case msg.GetPollCreationMessageV4().GetMessage() != nil:
			inner = msg.GetPollCreationMessageV4().GetMessage()
		case msg.GetEditedMessage().GetMessage() != nil:
			inner = msg.GetEditedMessage().GetMessage()
		default:
			return msg
		}
		msg = inner
	}
	return msg
}

func attachment(msg *waE2E.Message) (Attachment, bool) {
	msg = Unwrap(msg)
	var source interface {
		whatsmeow.DownloadableMessage
		GetURL() string
		GetFileLength() uint64
		GetMimetype() string
	}
	kind := Image
	filename := ""
	switch {
	case msg.GetImageMessage() != nil:
		source = msg.GetImageMessage()
	case msg.GetDocumentMessage() != nil:
		source, kind = msg.GetDocumentMessage(), Document
		filename = msg.GetDocumentMessage().GetFileName()
	case msg.GetAudioMessage() != nil:
		source, kind = msg.GetAudioMessage(), Audio
	case msg.GetVideoMessage() != nil:
		source, kind = msg.GetVideoMessage(), Video
	case msg.GetStickerMessage() != nil:
		source, kind = msg.GetStickerMessage(), Sticker
	default:
		return Attachment{}, false
	}
	if len(source.GetURL()) > 4096 || len(source.GetDirectPath()) > 4096 ||
		len(source.GetMediaKey()) > 64 || len(source.GetFileSHA256()) > 64 ||
		len(source.GetFileEncSHA256()) > 64 || len(source.GetMimetype()) > 256 {
		return Attachment{}, false
	}
	u := whatsmeow.UploadResponse{
		URL: source.GetURL(), DirectPath: source.GetDirectPath(), FileLength: source.GetFileLength(),
		MediaKey: bytes.Clone(source.GetMediaKey()), FileSHA256: bytes.Clone(source.GetFileSHA256()),
		FileEncSHA256: bytes.Clone(source.GetFileEncSHA256()),
	}
	// Direct paths use the SDK's WhatsApp media hosts. Never follow an arbitrary
	// URL supplied by a message sender, even though the descriptor is cached.
	if u.DirectPath != "" {
		if !strings.HasPrefix(u.DirectPath, "/") || strings.ContainsAny(u.DirectPath, "\\\r\n") {
			return Attachment{}, false
		}
		u.URL = ""
	} else if u.URL != "" {
		parsed, err := url.Parse(u.URL)
		if err != nil || parsed.Scheme != "https" || parsed.User != nil ||
			(parsed.Port() != "" && parsed.Port() != "443") ||
			!strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".whatsapp.net") {
			return Attachment{}, false
		}
	}
	// Rebuild a minimal descriptor: no thumbnails, quoted messages or context.
	minimal := Message(kind, u, source.GetMimetype(), Filename(filename), "")
	var downloadable whatsmeow.DownloadableMessage
	switch kind {
	case Image:
		downloadable = minimal.ImageMessage
	case Document:
		downloadable = minimal.DocumentMessage
	case Audio:
		downloadable = minimal.AudioMessage
	case Video:
		downloadable = minimal.VideoMessage
	case Sticker:
		downloadable = minimal.StickerMessage
	}
	return Attachment{Message: downloadable, Mimetype: source.GetMimetype(), Filename: Filename(filename), Length: u.FileLength}, true
}

// Descriptor rebuilds a bounded download descriptor from trusted session history.
func Descriptor(msg *waE2E.Message) (Attachment, bool) { return attachment(msg) }
