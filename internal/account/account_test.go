package account

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/polymorfa/hypermeow/types"
)

func TestBlocklistPreservesHTTPContractWithLIDSDK(t *testing.T) {
	pn := types.NewJID("123", types.DefaultUserServer)
	lid := types.NewJID("456", types.HiddenUserServer)
	result, err := blocklist(&types.Blocklist{DHash: "revision", Items: []types.BlocklistItem{
		{PN: pn, LID: lid}, {LID: lid},
	}}, nil)
	if err != nil || result.DHash != "revision" || len(result.JIDs) != 2 || result.JIDs[0] != pn || result.JIDs[1] != lid {
		t.Fatalf("blocklist contract changed: %+v, %v", result, err)
	}
	data, _ := json.Marshal(result)
	var wire map[string]json.RawMessage
	json.Unmarshal(data, &wire)
	if wire["JIDs"] == nil || wire["DHash"] == nil || wire["Items"] != nil {
		t.Fatal("legacy wire fields changed")
	}
	want := errors.New("upstream failure")
	if got, err := blocklist(nil, want); got != nil || !errors.Is(err, want) {
		t.Fatal("SDK error was swallowed")
	}
}
