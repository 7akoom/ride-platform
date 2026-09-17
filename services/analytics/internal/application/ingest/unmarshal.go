package ingest

import (
	"encoding/json"
	"fmt"
)

func unmarshal(payload []byte, dest any) error {
	if err := json.Unmarshal(payload, dest); err != nil {
		return fmt.Errorf("unmarshal event payload: %w", err)
	}
	return nil
}
