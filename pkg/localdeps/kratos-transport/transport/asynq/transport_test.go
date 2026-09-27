package asynq

import (
	"reflect"
	"testing"
)

func TestTransport_Kind(t *testing.T) {
	o := &Transport{}
	// Kind() returns the named transport.Kind, while the constant is untyped: a
	// direct comparison is what the contract is, and reflect.DeepEqual would
	// compare two different types and always report a difference.
	if got := o.Kind(); got != KindAsynq {
		t.Errorf("expect %v, got %v", KindAsynq, got)
	}
}

func TestTransport_Endpoint(t *testing.T) {
	v := "hello"
	o := &Transport{endpoint: v}
	if !reflect.DeepEqual(v, o.Endpoint()) {
		t.Errorf("expect %v, got %v", v, o.Endpoint())
	}
}

func TestTransport_Operation(t *testing.T) {
	v := "hello"
	o := &Transport{operation: v}
	if !reflect.DeepEqual(v, o.Operation()) {
		t.Errorf("expect %v, got %v", v, o.Operation())
	}
}
