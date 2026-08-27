package source

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestKnownSyncModels(t *testing.T) {
	tests := map[string]Direction{
		"supernote": TwoWay, "boox": OneWayIn, "forestnote": TwoWay, "remarkable": TwoWay,
	}
	for sourceType, direction := range tests {
		got := SyncModelFor(sourceType)
		if got.Label == "" || got.Direction != direction {
			t.Errorf("SyncModelFor(%q) = %+v", sourceType, got)
		}
	}
	if !reflect.DeepEqual(SyncModelFor("bogus"), Unmanaged) {
		t.Fatal("unknown source type did not return Unmanaged")
	}
}

func TestDirectionJSON(t *testing.T) {
	for direction, want := range map[Direction]string{TwoWay: `"two_way"`, OneWayIn: `"one_way_in"`} {
		data, err := json.Marshal(direction)
		if err != nil || string(data) != want {
			t.Fatalf("Marshal(%v) = %s, %v", direction, data, err)
		}
		var decoded Direction
		if err := json.Unmarshal(data, &decoded); err != nil || decoded != direction {
			t.Fatalf("Unmarshal(%s) = %v, %v", data, decoded, err)
		}
	}
	var invalid Direction
	if json.Unmarshal([]byte(`"sideways"`), &invalid) == nil {
		t.Fatal("unknown direction was accepted")
	}
}
