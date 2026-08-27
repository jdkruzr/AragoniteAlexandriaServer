// Package source holds portable source behavior shared by gateways and workers.
// The sync model was selectively ported from UltraBridge under Apache-2.0.
package source

import (
	"fmt"
	"strconv"
)

type Direction int

const (
	OneWayIn Direction = iota
	TwoWay
)

func (d Direction) String() string {
	switch d {
	case TwoWay:
		return "two_way"
	case OneWayIn:
		return "one_way_in"
	default:
		return "unknown"
	}
}

func (d Direction) MarshalJSON() ([]byte, error) { return []byte(strconv.Quote(d.String())), nil }

func (d *Direction) UnmarshalJSON(data []byte) error {
	value, err := strconv.Unquote(string(data))
	if err != nil {
		return fmt.Errorf("direction: expected string token: %w", err)
	}
	switch value {
	case "two_way":
		*d = TwoWay
	case "one_way_in":
		*d = OneWayIn
	default:
		return fmt.Errorf("direction: unknown token %q", value)
	}
	return nil
}

type SyncModel struct {
	Label            string    `json:"label"`
	Direction        Direction `json:"direction"`
	Authority        string    `json:"authority"`
	DeletesPropagate bool      `json:"deletes_propagate"`
	Blurb            string    `json:"blurb"`
}

func SyncModelFor(sourceType string) SyncModel {
	switch sourceType {
	case "supernote":
		return SyncModel{"Two-way sync", TwoWay, "Shared (Loom-hosted)", true, "Files sync both ways with your Supernote. Deleting a note moves it to a recoverable recycle bin."}
	case "boox":
		return SyncModel{"Receive-only", OneWayIn, "Device", false, "Boox exports notes to Loom one way. Device deletes and renames do not propagate; remove notes here manually."}
	case "forestnote":
		return SyncModel{"Live mirror", TwoWay, "Shared (row-level LWW)", true, "ForestNote mirrors notes two ways in real time. Recoverable tombstones converge across devices."}
	case "remarkable":
		return SyncModel{"Two-way sync", TwoWay, "Shared (reMarkable protocol)", true, "reMarkable devices sync through Loom's protocol surface and converge through the vendor sync model."}
	default:
		return Unmanaged
	}
}

var Unmanaged = SyncModel{"Unmanaged", OneWayIn, "Unknown", false, "This source type has no defined sync model."}
