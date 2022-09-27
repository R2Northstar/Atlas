// Package pdef parses Titanfall 2 player data definitions.
package pdef

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// Pdef describes the format of Titanfall 2 player data. Player data structures
// must not be recursive.
type Pdef struct {
	Root   []Field             `json:"root"`
	Enum   map[string][]string `json:"enum"`
	Struct map[string][]Field  `json:"struct"`
}

func (p Pdef) TypeSize(t TypeInfo) int {
	switch {
	case t.Int != nil:
		return 4 // int32le
	case t.Bool != nil:
		return 1 // byte
	case t.Float != nil:
		return 4 // float32le
	case t.String != nil:
		return t.String.Length // char[]
	case t.Array != nil:
		return t.Array.Length * p.TypeSize(t.Array.Type)
	case t.MappedArray != nil:
		if v, ok := p.Enum[t.MappedArray.Enum]; !ok {
			panic("undefined enum in pdef")
		} else {
			return len(v) * p.TypeSize(t.MappedArray.Type)
		}
	case t.Enum != nil:
		return 1 // uint8
	case t.Struct != nil:
		if fs, ok := p.Struct[t.Struct.Name]; !ok {
			panic("undefined struct in pdef")
		} else {
			var n int
			for _, f := range fs {
				n += p.TypeSize(f.Type)
			}
			return n
		}
	default:
		panic("internal unimplemented pdef type")
	}
}

// Field is a pdef entry.
type Field struct {
	Name string   `json:"name"`
	Type TypeInfo `json:"type"`
}

// TypeInfo describes a type. Exactly one of these struct fields will be set.
type TypeInfo struct {
	Int         *TypeInfoPrimitive   `json:"int,omitempty"`
	Bool        *TypeInfoPrimitive   `json:"bool,omitempty"`
	Float       *TypeInfoPrimitive   `json:"float,omitempty"`
	String      *TypeInfoString      `json:"string,omitempty"`
	Array       *TypeInfoArray       `json:"array,omitempty"`
	MappedArray *TypeInfoMappedArray `json:"mapped_array,omitempty"`
	Enum        *TypeInfoEnum        `json:"enum,omitempty"`
	Struct      *TypeInfoStruct      `json:"struct,omitempty"`
}

// TypeInfoPrimitive is used for an unconfigurable type.
type TypeInfoPrimitive struct {
}

// TypeInfoString is used for fixed-length strings. Note that nothing stops
// arbitrary binary data from being stored in a string.
type TypeInfoString struct {
	Length int `json:"length"`
}

// TypeInfoArray is used for a fixed-length array.
type TypeInfoArray struct {
	Type   TypeInfo `json:"type"`
	Length int      `json:"length"`
}

// TypeInfoArray is used for a fixed-length array mapping from an enum.
type TypeInfoMappedArray struct {
	Type TypeInfo `json:"type"`
	Enum string   `json:"enum"`
}

// TypeInfoEnum refers to a defined enum.
type TypeInfoEnum struct {
	Name string `json:"name"`
}

// TypeInfoEnum refers to a defined struct.
type TypeInfoStruct struct {
	Name string `json:"name"`
}

// ParsePdef parses the pdef from r. Most things are validated except for length
// limits.
func ParsePdef(r io.Reader) (*Pdef, error) {
	type state int
	const (
		stateRoot state = iota
		stateEnum
		stateStruct
	)
	var (
		pdef = Pdef{
			Root:   []Field{},
			Enum:   map[string][]string{},
			Struct: map[string][]Field{},
		}
		curLine  int
		curState state
		curName  string
	)
	var (
		ident   = "[a-zA-Z][a-zA-Z0-9_]*"
		identRe = regexp.MustCompile(`^(` + ident + `)$`)
		typeRe  = regexp.MustCompile(`^(` + ident + `)(?:\{([0-9]+)\})?$`)
		nameRe  = regexp.MustCompile(`^(` + ident + `)(?:\[(?:([0-9]+)|(` + ident + `))\])?$`)
	)
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		curLine++

		// tokenize by spaces
		line := strings.Fields(sc.Text())

		// remove comments
		for i, tok := range line {
			if strings.HasPrefix(tok, "//") {
				line = line[:i]
				break
			}
		}

		// ignore blank lines
		if len(line) == 0 {
			continue
		}

		// parse tokens
		switch tok, rest := line[0], line[1:]; tok {
		case "$ENUM_START":
			switch curState {
			case stateRoot:
				switch len(rest) {
				case 0:
					return nil, fmt.Errorf("line %d: expected enum name", curLine)
				case 1:
					if curState, curName = stateEnum, rest[0]; !identRe.MatchString(curName) {
						return nil, fmt.Errorf("line %d: invalid enum name %q", curLine, curName)
					}
					if _, exists := pdef.Struct[curName]; exists {
						return nil, fmt.Errorf("line %d: invalid enum name %q: struct already defined", curLine, curName)
					}
					if _, exists := pdef.Enum[curName]; exists {
						return nil, fmt.Errorf("line %d: invalid enum name %q: enum already defined", curLine, curName)
					}
					pdef.Enum[curName] = []string{}
				default:
					return nil, fmt.Errorf("line %d: unexpected token %q after enum name", curLine, rest[1])
				}
			case stateEnum:
				return nil, fmt.Errorf("line %d: cannot start enum within enum %q", curLine, curName)
			case stateStruct:
				return nil, fmt.Errorf("line %d: cannot start enum within struct %q", curLine, curName)
			}
		case "$ENUM_END":
			switch curState {
			case stateRoot, stateStruct:
				return nil, fmt.Errorf("line %d: not in an enum", curLine)
			case stateEnum:
				switch len(rest) {
				case 0:
					curState = stateRoot
					curName = ""
				default:
					return nil, fmt.Errorf("line %d: unexpected token %q after enum end", curLine, rest[1])
				}
			}
		case "$STRUCT_START":
			switch curState {
			case stateRoot:
				switch len(rest) {
				case 0:
					return nil, fmt.Errorf("line %d: expected struct name", curLine)
				case 1:
					if curState, curName = stateStruct, rest[0]; !identRe.MatchString(curName) {
						return nil, fmt.Errorf("line %d: invalid struct name %q", curLine, curName)
					}
					if _, exists := pdef.Struct[curName]; exists {
						return nil, fmt.Errorf("line %d: invalid struct name %q: struct already defined", curLine, curName)
					}
					if _, exists := pdef.Enum[curName]; exists {
						return nil, fmt.Errorf("line %d: invalid struct name %q: enum already defined", curLine, curName)
					}
					pdef.Struct[curName] = []Field{}
				default:
					return nil, fmt.Errorf("line %d: unexpected token %q after struct name", curLine, rest[1])
				}
			case stateEnum:
				return nil, fmt.Errorf("line %d: cannot start struct within enum %q", curLine, curName)
			case stateStruct:
				return nil, fmt.Errorf("line %d: cannot start struct within struct %q", curLine, curName)
			}
		case "$STRUCT_END":
			switch curState {
			case stateRoot, stateEnum:
				return nil, fmt.Errorf("line %d: not in an struct", curLine)
			case stateStruct:
				switch len(rest) {
				case 0:
					curState = stateRoot
					curName = ""
				default:
					return nil, fmt.Errorf("line %d: unexpected token %q after struct end", curLine, rest[1])
				}
			}
		default:
			if tok[0] == '$' {
				return nil, fmt.Errorf("line %d: invalid keyword %q", curLine, tok)
			}
			switch curState {
			case stateRoot, stateStruct:
				switch len(rest) {
				case 0:
					return nil, fmt.Errorf("line %d: expected field type then name", curLine)
				case 1:
					var m1, m2 []string
					if m1 = typeRe.FindStringSubmatch(tok); m1 == nil {
						return nil, fmt.Errorf("line %d: invalid type %q", curLine, tok)
					}
					if m2 = nameRe.FindStringSubmatch(rest[0]); m2 == nil {
						return nil, fmt.Errorf("line %d: invalid name %q", curLine, rest[0])
					}

					var typeinfo TypeInfo
					switch typename := m1[1]; typename {
					case "int":
						typeinfo.Int = &TypeInfoPrimitive{}
					case "bool":
						typeinfo.Bool = &TypeInfoPrimitive{}
					case "float":
						typeinfo.Float = &TypeInfoPrimitive{}
					case "string":
						if m1[2] == "" {
							return nil, fmt.Errorf("line %d: invalid type %q: missing size", curLine, tok)
						}
						if n, err := strconv.Atoi(m1[2]); err != nil {
							panic(err)
						} else if n < 1 {
							return nil, fmt.Errorf("line %d: invalid type %q: invalid size %s", curLine, tok, m1[2])
						} else {
							typeinfo.String = &TypeInfoString{
								Length: n,
							}
						}
					default:
						if typename == curName {
							return nil, fmt.Errorf("line %d: invalid type %q: recursive types are not supported", curLine, tok)
						}
						if _, ok := pdef.Enum[typename]; ok {
							typeinfo.Enum = &TypeInfoEnum{
								Name: typename,
							}
						} else if _, ok := pdef.Struct[typename]; ok {
							typeinfo.Struct = &TypeInfoStruct{
								Name: typename,
							}
						} else {
							return nil, fmt.Errorf("line %d: invalid type %q: unknown enum/struct %q", curLine, tok, m1[1])
						}
					}

					var field Field
					switch {
					case m2[2] != "":
						if n, err := strconv.Atoi(m2[2]); err != nil {
							panic(err)
						} else if n < 1 {
							return nil, fmt.Errorf("line %d: invalid type %q: invalid size %s", curLine, tok, m1[2])
						} else {
							field.Name = m2[1]
							field.Type.Array = &TypeInfoArray{
								Type:   typeinfo,
								Length: n,
							}
						}
					case m2[3] != "":
						if _, ok := pdef.Enum[m2[3]]; ok {
							field.Name = m2[1]
							field.Type.MappedArray = &TypeInfoMappedArray{
								Type: typeinfo,
								Enum: m2[3],
							}
						} else {
							return nil, fmt.Errorf("line %d: unknown enum/struct %q", curLine, m2[3])
						}
					default:
						field.Name = m2[1]
						field.Type = typeinfo
					}

					if curState == stateStruct {
						pdef.Struct[curName] = append(pdef.Struct[curName], field)
					} else {
						pdef.Root = append(pdef.Root, field)
					}
				default:
					return nil, fmt.Errorf("line %d: unexpected token %q after field name", curLine, rest[2])
				}
			case stateEnum:
				switch len(rest) {
				case 0:
					pdef.Enum[curName] = append(pdef.Enum[curName], tok)
				default:
					return nil, fmt.Errorf("line %d: unexpected token %q after enum value", curLine, rest[1])
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return &pdef, nil
}
