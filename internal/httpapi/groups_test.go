package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wppconnect-team/wppconnect-server-go/internal/auth"
	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"github.com/wppconnect-team/wppconnect-server-go/internal/groups"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

type groupCall struct {
	operation string
	id        types.JID
	people    []types.JID
	value     any
}
type fakeGroupClient struct {
	info             *types.GroupInfo
	list             []*types.GroupInfo
	calls            []groupCall
	failure          error
	failAfter        int
	updates          []types.GroupParticipant
	customUpdates    bool
	creationFailures bool
}

func (f *fakeGroupClient) record(ctx context.Context, op string, id types.JID, value any) error {
	if _, ok := ctx.Deadline(); !ok {
		panic("group operation has no timeout")
	}
	f.calls = append(f.calls, groupCall{operation: op, id: id, value: value})
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.failAfter == 0 || len(f.calls) >= f.failAfter {
		return f.failure
	}
	return nil
}
func (f *fakeGroupClient) GetJoinedGroups(ctx context.Context) ([]*types.GroupInfo, error) {
	return f.list, f.record(ctx, "list", types.EmptyJID, nil)
}
func (f *fakeGroupClient) GetGroupInfo(ctx context.Context, id types.JID) (*types.GroupInfo, error) {
	return f.info, f.record(ctx, "info", id, nil)
}
func (f *fakeGroupClient) CreateGroup(ctx context.Context, req whatsmeow.ReqCreateGroup) (*types.GroupInfo, error) {
	err := f.record(ctx, "create", types.EmptyJID, req)
	people := make([]types.GroupParticipant, 0, len(req.Participants))
	for _, p := range req.Participants {
		code := 0
		if f.creationFailures {
			code = 403
		}
		people = append(people, types.GroupParticipant{JID: p, Error: code})
	}
	return &types.GroupInfo{JID: types.NewJID("999", types.GroupServer), GroupName: types.GroupName{Name: req.Name}, Participants: people}, err
}
func (f *fakeGroupClient) UpdateGroupParticipants(ctx context.Context, id types.JID, people []types.JID, action whatsmeow.ParticipantChange) ([]types.GroupParticipant, error) {
	err := f.record(ctx, "participants", id, action)
	f.calls[len(f.calls)-1].people = people
	if f.customUpdates {
		return f.updates, err
	}
	result := make([]types.GroupParticipant, 0, len(people))
	for _, p := range people {
		result = append(result, types.GroupParticipant{JID: p})
	}
	return result, err
}
func (f *fakeGroupClient) SetGroupName(ctx context.Context, id types.JID, value string) error {
	return f.record(ctx, "subject", id, value)
}
func (f *fakeGroupClient) SetGroupDescription(ctx context.Context, id types.JID, value string) error {
	return f.record(ctx, "description", id, value)
}
func (f *fakeGroupClient) SetGroupLocked(ctx context.Context, id types.JID, value bool) error {
	return f.record(ctx, "restrict", id, value)
}
func (f *fakeGroupClient) SetGroupAnnounce(ctx context.Context, id types.JID, value bool) error {
	return f.record(ctx, "announcement", id, value)
}
func (f *fakeGroupClient) SetDisappearingTimer(ctx context.Context, id types.JID, value time.Duration, _ time.Time) error {
	return f.record(ctx, "ephemeral", id, value)
}
func (f *fakeGroupClient) GetGroupInviteLink(ctx context.Context, id types.JID, reset bool) (string, error) {
	return "https://chat.whatsapp.com/InviteCode123", f.record(ctx, "invite", id, reset)
}
func (f *fakeGroupClient) GetGroupInfoFromLink(ctx context.Context, code string) (*types.GroupInfo, error) {
	return f.info, f.record(ctx, "invite-info", types.EmptyJID, code)
}
func (f *fakeGroupClient) JoinGroupWithLink(ctx context.Context, code string) (types.JID, error) {
	return f.info.JID, f.record(ctx, "join", types.EmptyJID, code)
}
func (f *fakeGroupClient) LeaveGroup(ctx context.Context, id types.JID) error {
	return f.record(ctx, "leave", id, nil)
}
func (f *fakeGroupClient) SetGroupPhoto(ctx context.Context, id types.JID, value []byte) (string, error) {
	return "photo-id", f.record(ctx, "photo", id, bytes.Clone(value))
}

type groupHarness struct {
	router  http.Handler
	clients map[string]*fakeGroupClient
	tokens  map[string]string
}

func newGroupHarness(t *testing.T) *groupHarness {
	t.Helper()
	h := &groupHarness{clients: make(map[string]*fakeGroupClient), tokens: make(map[string]string)}
	for _, name := range []string{"alpha", "beta"} {
		info := &types.GroupInfo{JID: types.NewJID("123-456", types.GroupServer), GroupName: types.GroupName{Name: name, NameSetAt: time.Unix(1000, 0), NameSetBy: types.NewJID("5511001", types.DefaultUserServer)}, GroupTopic: types.GroupTopic{Topic: "description"}, Participants: []types.GroupParticipant{
			{JID: types.NewJID("5511001", types.DefaultUserServer), IsAdmin: true},
			{JID: types.NewJID("5511002", types.DefaultUserServer), IsSuperAdmin: true},
			{JID: types.NewJID("999001", types.HiddenUserServer), PhoneNumber: types.NewJID("5511003", types.DefaultUserServer)},
		}}
		h.clients[name] = &fakeGroupClient{info: info, list: []*types.GroupInfo{info}}
		token, err := auth.Generate(name, "group-secret")
		if err != nil {
			t.Fatal(err)
		}
		h.tokens[name] = token
	}
	s := &Server{cfg: config.Config{SecretKey: "group-secret"}, groupSession: func(name string) (groups.Client, bool) { client, ok := h.clients[name]; return client, ok }}
	h.router = s.router()
	return h
}
func (h *groupHarness) call(method, path, token string, body io.Reader) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, body)
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, r)
	return w
}
func (h *groupHarness) post(t *testing.T, route string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return h.call(http.MethodPost, "/api/alpha/"+route, h.tokens["alpha"], bytes.NewReader(body))
}
func groupResponse(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestGroupReadContracts(t *testing.T) {
	h := newGroupHarness(t)
	for _, route := range []string{"all-groups", "group-members/123-456", "group-members-ids/123-456@g.us", "group-admins/123-456", "group-info/123-456"} {
		t.Run(route, func(t *testing.T) {
			w := h.call("GET", "/api/alpha/"+route, h.tokens["alpha"], nil)
			if w.Code != 200 {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			result := groupResponse(t, w)
			if result["status"] != "success" {
				t.Fatal(result)
			}
			switch strings.Split(route, "/")[0] {
			case "all-groups":
				list := result["response"].([]any)
				if list[0].(map[string]any)["participants"] != float64(3) {
					t.Fatal(result)
				}
			case "group-info":
				info := result["response"].(map[string]any)
				if info["id"] != "123-456@g.us" || info["subject"] != "alpha" || info["description"] != "description" || info["lastActivityAt"] != nil || info["subjectUpdatedAt"] != "1970-01-01T00:16:40.000Z" {
					t.Fatal(info)
				}
			case "group-admins":
				arrays := result["response"].([]any)
				if len(arrays) != 1 || len(arrays[0].([]any)) != 2 {
					t.Fatal(result)
				}
			default:
				members := result["response"].([]any)
				id := members[0].(map[string]any)
				if strings.HasPrefix(route, "group-members/") {
					id = id["id"].(map[string]any)
				}
				if id["_serialized"] != "5511001@c.us" {
					t.Fatal(result)
				}
			}
		})
	}
	w := h.call("GET", "/api/alpha/common-groups/5511003@c.us", h.tokens["alpha"], nil)
	var common []any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &common) != nil || len(common) != 1 {
		t.Fatalf("common groups with LID mapping: %d %s", w.Code, w.Body.String())
	}
}

func TestCreateGroupsAndPartialParticipants(t *testing.T) {
	h := newGroupHarness(t)
	w := h.post(t, "create-group", map[string]any{"name": []string{"Team A", "Team B"}, "participants": []string{"+55 (11) 001", "5511002@c.us", "999001@lid"}})
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	response := groupResponse(t, w)["response"].(map[string]any)
	if len(response["groupInfo"].([]any)) != 2 {
		t.Fatal(response)
	}
	for _, call := range h.clients["alpha"].calls {
		req := call.value.(whatsmeow.ReqCreateGroup)
		if len(req.Participants) != 3 || req.Participants[0].String() != "5511001@s.whatsapp.net" || req.Participants[2].Server != types.HiddenUserServer {
			t.Fatal(req)
		}
	}
	h.clients["alpha"].creationFailures = true
	w = h.post(t, "create-group", map[string]any{"name": "Created but not all added", "participants": "5511001"})
	if w.Code != 201 || groupResponse(t, w)["status"] != "partial" {
		t.Fatalf("creation masked rejected participants: %d %s", w.Code, w.Body.String())
	}
}

func TestGroupParticipantActions(t *testing.T) {
	for _, tc := range []struct {
		route  string
		action whatsmeow.ParticipantChange
		code   int
	}{
		{"add-participant-group", whatsmeow.ParticipantChangeAdd, 201}, {"remove-participant-group", whatsmeow.ParticipantChangeRemove, 200},
		{"promote-participant-group", whatsmeow.ParticipantChangePromote, 201}, {"demote-participant-group", whatsmeow.ParticipantChangeDemote, 201},
	} {
		t.Run(tc.route, func(t *testing.T) {
			h := newGroupHarness(t)
			w := h.post(t, tc.route, map[string]any{"groupId": []string{"123-456", "987@g.us"}, "phone": "5511001;999001@lid"})
			if w.Code != tc.code {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			calls := h.clients["alpha"].calls
			if len(calls) != 2 || calls[0].value != tc.action || calls[1].id.String() != "987@g.us" || calls[0].people[1].Server != types.HiddenUserServer {
				t.Fatal(calls)
			}
		})
	}
}

func TestGroupParticipantRejectionsNotReportedAsSuccess(t *testing.T) {
	h := newGroupHarness(t)
	f := h.clients["alpha"]
	f.customUpdates = true
	f.updates = []types.GroupParticipant{{JID: types.NewJID("5511001", types.DefaultUserServer)}, {JID: types.NewJID("5511002", types.DefaultUserServer), Error: 403}}
	w := h.post(t, "add-participant-group", map[string]any{"groupId": "123-456", "phone": []string{"5511001", "5511002"}})
	if w.Code != 207 || groupResponse(t, w)["status"] != "partial" {
		t.Fatalf("partial rejected: %d %s", w.Code, w.Body.String())
	}
	f.updates = f.updates[1:]
	w = h.post(t, "promote-participant-group", map[string]any{"groupId": "123-456", "phone": "5511002"})
	if w.Code != 403 || groupResponse(t, w)["status"] != "error" {
		t.Fatalf("forbidden masked: %d %s", w.Code, w.Body.String())
	}
	f.updates = nil
	w = h.post(t, "remove-participant-group", map[string]any{"groupId": "123-456", "phone": "5511001"})
	if w.Code != 502 {
		t.Fatalf("missing operation result treated as success: %d %s", w.Code, w.Body.String())
	}
}

func TestGroupMetadataContracts(t *testing.T) {
	h := newGroupHarness(t)
	for _, tc := range []struct {
		route, operation string
		payload          map[string]any
		value            any
	}{
		{"group-subject", "subject", map[string]any{"title": "New title"}, "New title"},
		{"group-description", "description", map[string]any{"description": ""}, ""},
		{"group-property", "restrict", map[string]any{"property": "restrict", "value": false}, false},
		{"group-property", "announcement", map[string]any{"property": "announcement"}, true},
		{"messages-admins-only", "announcement", map[string]any{"value": false}, false},
		{"change-privacy-group", "restrict", map[string]any{"status": "false"}, false},
		{"change-privacy-group", "restrict", map[string]any{"status": true}, true},
		{"group-property", "ephemeral", map[string]any{"property": "ephemeral", "value": 86400}, 24 * time.Hour},
		{"group-property", "ephemeral", map[string]any{"property": "ephemeral", "value": false}, time.Duration(0)},
		{"leave-group", "leave", map[string]any{}, nil},
	} {
		t.Run(fmt.Sprint(tc.route, tc.value), func(t *testing.T) {
			tc.payload["groupId"] = "123-456@g.us"
			w := h.post(t, tc.route, tc.payload)
			if w.Code != 200 {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			calls := h.clients["alpha"].calls
			call := calls[len(calls)-1]
			if call.operation != tc.operation || call.value != tc.value || call.id.String() != "123-456@g.us" {
				t.Fatal(call)
			}
		})
	}
}

func TestGroupInviteContracts(t *testing.T) {
	h := newGroupHarness(t)
	for _, route := range []string{"group-invite-link", "group-revoke-link"} {
		w := h.call("GET", "/api/alpha/"+route+"/123-456", h.tokens["alpha"], nil)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		call := h.clients["alpha"].calls[len(h.clients["alpha"].calls)-1]
		if call.value != (route == "group-revoke-link") {
			t.Fatal(call)
		}
	}
	for _, tc := range []struct {
		route, field string
		code         int
	}{{"join-code", "inviteCode", 201}, {"group-info-from-invite-link", "invitecode", 200}} {
		w := h.post(t, tc.route, map[string]any{tc.field: "https://chat.whatsapp.com/invite/InviteCode123?mode=test"})
		if w.Code != tc.code {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		call := h.clients["alpha"].calls[len(h.clients["alpha"].calls)-1]
		if call.value != "InviteCode123" {
			t.Fatal(call)
		}
	}
}

func TestGroupInputLimitsAndValidation(t *testing.T) {
	h := newGroupHarness(t)
	for _, payload := range []any{
		map[string]any{"name": strings.TrimSuffix(strings.Repeat("Team,", 11), ","), "participants": "123"},
		map[string]any{"name": "Team", "participants": strings.TrimSuffix(strings.Repeat("123,", 101), ",")},
	} {
		if w := h.post(t, "create-group", payload); w.Code != 400 {
			t.Fatalf("creation list limit: %d %s", w.Code, w.Body.String())
		}
	}
	if w := h.post(t, "group-subject", map[string]any{"groupId": strings.TrimSuffix(strings.Repeat("123,", 21), ","), "title": "Team"}); w.Code != 400 {
		t.Fatalf("group count limit: %d", w.Code)
	}
	for _, tc := range []struct {
		route   string
		payload any
	}{
		{"create-group", map[string]any{"name": "Team", "participants": []string{}}},
		{"create-group", map[string]any{"name": strings.Repeat("é", 101), "participants": "123"}},
		{"create-group", map[string]any{"name": "Team", "participants": []any{"123", 42}}},
		{"add-participant-group", map[string]any{"groupId": "123@c.us", "phone": "123"}},
		{"add-participant-group", map[string]any{"groupId": "123", "phone": "123@g.us"}},
		{"group-subject", map[string]any{"groupId": "123", "title": " "}},
		{"group-description", map[string]any{"groupId": "123", "description": strings.Repeat("x", 2049)}},
		{"group-property", map[string]any{"groupId": "123", "property": "unknown"}},
		{"group-property", map[string]any{"groupId": "123", "property": "restrict", "value": "false"}},
		{"group-property", map[string]any{"groupId": "123", "property": "ephemeral", "value": 123}},
		{"group-property", map[string]any{"groupId": "123", "property": "ephemeral", "value": nil}},
		{"change-privacy-group", map[string]any{"groupId": "123"}},
		{"join-code", map[string]any{"inviteCode": "https://evil.test/InviteCode123"}},
		{"group-info-from-invite-link", map[string]any{"invitecode": "https://chat.whatsapp.com@evil.test/InviteCode123"}},
	} {
		w := h.post(t, tc.route, tc.payload)
		if w.Code != 400 {
			t.Fatalf("%s: %d %s", tc.route, w.Code, w.Body.String())
		}
	}
	w := h.call("POST", "/api/alpha/group-subject", h.tokens["alpha"], strings.NewReader(`{"groupId":"123","title":"a"} {}`))
	if w.Code != 400 {
		t.Fatalf("trailing JSON accepted: %d", w.Code)
	}
	w = h.call("POST", "/api/alpha/group-subject", h.tokens["alpha"], io.MultiReader(strings.NewReader(`{"groupId":"123","title":"a"}`), strings.NewReader(strings.Repeat(" ", groups.MaxBodyBytes))))
	if w.Code != 413 {
		t.Fatalf("streaming body limit: %d %s", w.Code, w.Body.String())
	}
	if len(h.clients["alpha"].calls) != 0 {
		t.Fatal("invalid request reached WhatsApp")
	}
}

func TestGroupErrorMappingAndCompletedWork(t *testing.T) {
	h := newGroupHarness(t)
	for _, tc := range []struct {
		err  error
		code int
	}{
		{whatsmeow.ErrNotInGroup, 403}, {whatsmeow.ErrGroupNotFound, 404}, {fmt.Errorf("wrapped: %w", whatsmeow.ErrIQForbidden), 403},
		{whatsmeow.ErrInviteLinkRevoked, 410}, {whatsmeow.ErrIQRateOverLimit, 429}, {whatsmeow.ErrIQResourceLimit, 429},
		{context.DeadlineExceeded, 504}, {context.Canceled, 408}, {whatsmeow.ErrNotConnected, 503}, {errors.New("network private details"), 502},
	} {
		h.clients["alpha"].failure = tc.err
		w := h.post(t, "group-description", map[string]any{"groupId": "123", "description": "text"})
		if w.Code != tc.code || strings.Contains(w.Body.String(), "private details") {
			t.Fatalf("%v: %d %s", tc.err, w.Code, w.Body.String())
		}
	}
	f := h.clients["alpha"]
	f.calls = nil
	f.failure = whatsmeow.ErrIQForbidden
	f.failAfter = 2
	w := h.post(t, "group-subject", map[string]any{"groupId": []string{"123", "456"}, "title": "update"})
	if w.Code != 403 || !strings.Contains(w.Body.String(), `"completed":["123@g.us"]`) {
		t.Fatalf("lost completed work: %d %s", w.Code, w.Body.String())
	}
}

func TestGroupAuthenticationAndSessionIsolation(t *testing.T) {
	h := newGroupHarness(t)
	for _, tc := range []struct {
		path, token string
		code        int
	}{
		{"/api/alpha/group-info/123", h.tokens["beta"], 401}, {"/api/alpha/group-info/123", "", 401},
		{"/api/beta/group-info/123", h.tokens["beta"], 200},
	} {
		w := h.call("GET", tc.path, tc.token, nil)
		if w.Code != tc.code {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		if tc.code == 200 && !strings.Contains(w.Body.String(), `"name":"beta"`) {
			t.Fatal("wrong session used")
		}
	}
	if len(h.clients["alpha"].calls) != 0 || len(h.clients["beta"].calls) != 1 {
		t.Fatal("request escaped session boundary")
	}
	delete(h.clients, "alpha")
	w := h.post(t, "create-group", map[string]any{"name": "Team", "participants": "123"})
	if w.Code != 404 {
		t.Fatalf("disconnected: %d", w.Code)
	}
}

func TestGroupPicture(t *testing.T) {
	h := newGroupHarness(t)
	w := h.post(t, "group-pic", map[string]any{"groupId": "123-456", "base64": base64.StdEncoding.EncodeToString(pngData(t))})
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	call := h.clients["alpha"].calls[0]
	if _, err := jpeg.Decode(bytes.NewReader(call.value.([]byte))); err != nil || call.id.String() != "123-456@g.us" {
		t.Fatalf("picture is not JPEG: %v", err)
	}
	photo, err := jpeg.DecodeConfig(bytes.NewReader(call.value.([]byte)))
	if err != nil || photo.Width != 640 || photo.Height != 640 {
		t.Fatalf("picture dimensions: %+v %v", photo, err)
	}
	w = h.post(t, "group-pic", map[string]any{"groupId": "123", "base64": "aGk="})
	if w.Code != 400 {
		t.Fatalf("bad photo: %d", w.Code)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("groupId", "123-456")
	file, err := form.CreateFormFile("file", "photo.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write(pngData(t))
	_ = form.Close()
	r := httptest.NewRequest("POST", "/api/alpha/group-pic", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.Header.Set("Authorization", "Bearer "+h.tokens["alpha"])
	w = httptest.NewRecorder()
	h.router.ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatalf("multipart group picture: %d %s", w.Code, w.Body.String())
	}
}

func TestGroupBatchCreationFailureAndEncodedQueries(t *testing.T) {
	h := newGroupHarness(t)
	f := h.clients["alpha"]
	f.failure = whatsmeow.ErrIQForbidden
	f.failAfter = 2
	w := h.post(t, "create-group", map[string]any{"name": []string{"Created", "Failed"}, "participants": "123"})
	if w.Code != 403 || len(groupResponse(t, w)["completed"].([]any)) != 1 {
		t.Fatalf("created group ID lost: %d %s", w.Code, w.Body.String())
	}
	f.failure = nil
	f.calls = nil
	w = h.call("GET", "/api/alpha/group-admins/123%2C456", h.tokens["alpha"], nil)
	if w.Code != 200 || len(f.calls) != 2 || f.calls[1].id.String() != "456@g.us" {
		t.Fatalf("encoded query: %d %s", w.Code, w.Body.String())
	}
	f.info = nil
	w = h.call("GET", "/api/alpha/group-info/123", h.tokens["alpha"], nil)
	if w.Code != 502 {
		t.Fatalf("missing metadata treated as success: %d", w.Code)
	}
}
