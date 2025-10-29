package dsnet

import (
	"encoding/json"

	structpb "google.golang.org/protobuf/types/known/structpb"
)

func encodeStruct(v any) (*structpb.Struct, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return structpb.NewStruct(m)
}

func decodeStruct(s *structpb.Struct, v any) error {
	data, err := json.Marshal(s.AsMap())
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
