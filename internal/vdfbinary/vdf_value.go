// Package vdfbinary parses Valve's binary VDF format.
//
// This is a vendored and modified version of github.com/TimDeve/valve-vdf-binary
// Licensed under MIT. See LICENSE file in this directory.
package vdfbinary

const (
	vdfMarkerMap         byte = 0x00
	vdfMarkerString      byte = 0x01
	vdfMarkerNumber      byte = 0x02
	vdfMarkerEndOfMap    byte = 0x08
	vdfMarkerEndOfString byte = 0x00
)

type VdfMap map[string]VdfValue

type vdfValueTypes interface {
	uint32 | string | VdfMap
}

type VdfValue interface {
	AsString() (string, bool)
	GetString(key string) (string, bool)
	AsUint() (uint32, bool)
	GetUint(key string) (uint32, bool)
	AsInt() (int, bool)
	GetInt(key string) (int, bool)
	AsFloat() (float32, bool)
	GetFloat(key string) (float32, bool)
	AsBool() (value, ok bool)
	GetBool(key string) (value, ok bool)
	AsMap() (VdfMap, bool)
	GetMap(key string) (VdfMap, bool)
}

type vdfValue struct {
	data any
}

func MakeVdfValue[D vdfValueTypes](s D) VdfValue {
	return vdfValue{s}
}

func (sv vdfValue) AsString() (string, bool) {
	s, ok := sv.data.(string)
	return s, ok
}

func (sv vdfValue) GetString(key string) (string, bool) {
	m, ok := sv.AsMap()
	if !ok {
		return "", ok
	}
	v, ok := m[key]
	if !ok {
		return "", ok
	}
	return v.AsString()
}

func (sv vdfValue) AsUint() (uint32, bool) {
	i, ok := sv.data.(uint32)
	return i, ok
}

func (sv vdfValue) GetUint(key string) (uint32, bool) {
	m, ok := sv.AsMap()
	if !ok {
		return 0, ok
	}
	v, ok := m[key]
	if !ok {
		return 0, ok
	}
	return v.AsUint()
}

func (sv vdfValue) AsInt() (int, bool) {
	i, ok := sv.data.(uint32)
	return int(i), ok
}

func (sv vdfValue) GetInt(key string) (int, bool) {
	m, ok := sv.AsMap()
	if !ok {
		return 0, ok
	}
	v, ok := m[key]
	if !ok {
		return 0, ok
	}
	return v.AsInt()
}

func (sv vdfValue) AsFloat() (float32, bool) {
	f, ok := sv.data.(uint32)
	return float32(f), ok
}

func (sv vdfValue) GetFloat(key string) (float32, bool) {
	m, ok := sv.AsMap()
	if !ok {
		return 0, ok
	}
	v, ok := m[key]
	if !ok {
		return 0, ok
	}
	return v.AsFloat()
}

func (sv vdfValue) AsBool() (value, ok bool) {
	i, ok := sv.data.(uint32)
	if i == 0 {
		return false, ok
	}
	return true, ok
}

func (sv vdfValue) GetBool(key string) (value, ok bool) {
	m, ok := sv.AsMap()
	if !ok {
		return false, ok
	}
	v, ok := m[key]
	if !ok {
		return false, ok
	}
	return v.AsBool()
}

func (sv vdfValue) AsMap() (VdfMap, bool) {
	m, ok := sv.data.(VdfMap)
	return m, ok
}

func (sv vdfValue) GetMap(key string) (VdfMap, bool) {
	m, ok := sv.AsMap()
	if !ok {
		return nil, ok
	}
	v, ok := m[key]
	if !ok {
		return nil, ok
	}
	return v.AsMap()
}
