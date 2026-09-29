package repository

import (
	"encoding/json"

	"github.com/Abshoor24/backend-go/internal/modules/project/model"
)

func briefJSONMarshal(b model.Brief) ([]byte, error) {
	return json.Marshal(b)
}

func briefJSONUnmarshal(data []byte, b *model.Brief) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, b)
}
