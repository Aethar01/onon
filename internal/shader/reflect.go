package shader

// Usage is what of its inputs a compiled shader reads, which says what
// drawing it needs: a shader that reads no time looks the same until its
// box changes, and one that reads no backdrop needs none copied for it.
type Usage struct {
	Time     bool // it reads iTime
	Backdrop bool // it samples iChannel0
	Beyond   bool // it calls iBackdrop, reading what lies round its box too
}

// SPIR-V's opcodes and decoration read here.
const (
	opConstant          = 43
	opFunctionCall      = 57
	opLoad              = 61
	opAccessChain       = 65
	opInBoundsAccess    = 66
	opDecorate          = 71
	decorDescriptorSet  = 34
	setTextures         = 2 // where the preamble puts iChannel0
	setUniforms         = 3 // and its uniform block
	uniformsTimeMember  = 1 // iTime's place in the block
	spirvHeaderWords    = 5
	spirvWordCountShift = 16
)

// Reflect is what spirv, a fragment shader Wrap wrote compiled by SPIRV,
// reads. The compiler has already dropped comments and what the
// preprocessor left out, so a mention of iTime that is never read does not
// count; one in code that never runs still does.
func Reflect(spirv []uint32) Usage {
	var u Usage
	if len(spirv) < spirvHeaderWords {
		return Usage{Time: true, Backdrop: true}
	}
	sets := map[uint32]uint32{}      // variables by descriptor set
	constants := map[uint32]uint32{} // integer constants' values by id
	type access struct{ base, index uint32 }
	var accesses []access
	var loads []uint32 // pointers loaded or passed to a function
	for i := spirvHeaderWords; i < len(spirv); {
		count := int(spirv[i] >> spirvWordCountShift)
		op := spirv[i] & 0xffff
		if count == 0 || i+count > len(spirv) {
			return Usage{Time: true, Backdrop: true} // malformed: assume all
		}
		w := spirv[i : i+count]
		switch {
		case op == opDecorate && count >= 4 && w[2] == decorDescriptorSet:
			sets[w[1]] = w[3]
		case op == opConstant && count >= 4:
			constants[w[2]] = w[3]
		case (op == opAccessChain || op == opInBoundsAccess) && count >= 5:
			accesses = append(accesses, access{w[3], w[4]})
			loads = append(loads, w[3])
		case op == opLoad && count >= 4:
			loads = append(loads, w[3])
		case op == opFunctionCall && count >= 4:
			loads = append(loads, w[4:]...)
		}
		i += count
	}
	for _, a := range accesses {
		if set, ok := sets[a.base]; ok && set == setUniforms {
			if v, ok := constants[a.index]; ok && v == uniformsTimeMember {
				u.Time = true
			}
		}
	}
	for _, p := range loads {
		if set, ok := sets[p]; ok && set == setTextures {
			u.Backdrop = true
		}
	}
	return u
}
