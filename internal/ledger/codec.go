package ledger

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var ErrSchema = errors.New("ledger: unsupported schema")

type document struct {
	Schema int    `json:"schema"`
	State  *State `json:"state"`
}

func Encode(state State) ([]byte, error) {
	if err := validateState(state); err != nil {
		return nil, err
	}
	return json.Marshal(document{Schema: Schema, State: &state})
}

func Decode(data []byte) (State, error) {
	var doc document
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return State{}, fmt.Errorf("%w: %w", ErrInvalidState, err)
	}
	if doc.Schema != Schema {
		return State{}, fmt.Errorf("%w: %d", ErrSchema, doc.Schema)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = errors.New("ledger: trailing JSON value")
		}
		return State{}, fmt.Errorf("%w: %w", ErrInvalidState, err)
	}
	if doc.State == nil {
		return State{}, fmt.Errorf("%w: missing state document", ErrInvalidState)
	}
	if err := validateState(*doc.State); err != nil {
		return State{}, err
	}
	return *doc.State, nil
}
