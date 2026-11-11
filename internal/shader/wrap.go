package shader

import (
	"encoding/binary"
	"math"
)

// preamble is what a theme's shader is given, as Shadertoy gives its own:
// the backdrop as iChannel0, the box's size as iResolution and the time as
// iTime. The names starting onon_ are the wrapper's own.
const preamble = `#version 450
layout(location = 0) in vec4 onon_color;
layout(location = 1) in vec2 onon_uv;
layout(location = 0) out vec4 onon_out;
layout(set = 2, binding = 0) uniform sampler2D iChannel0;
layout(set = 3, binding = 0, std140) uniform onon_uniforms {
	vec3 iResolution;
	float iTime;
	vec4 iRadius;
	float iScale;
	float iAlt;
};

// iShape is how far coord is from the edge of the element's shape, in
// pixels: negative inside it.
float iShape(vec2 coord) {
	vec2 half_size = iResolution.xy * 0.5;
	vec2 p = coord - half_size;
	float r = p.x < 0.0 ? (p.y > 0.0 ? iRadius.x : iRadius.w) : (p.y > 0.0 ? iRadius.y : iRadius.z);
	vec2 d = abs(p) - half_size + r;
	return min(max(d.x, d.y), 0.0) + length(max(d, 0.0)) - r;
}
#line 1
`

// epilogue runs the theme's mainImage for a pixel and cuts what it gives
// to the element's shape, at the element's opacity.
const epilogue = `
void main() {
	vec2 coord = vec2(onon_uv.x, 1.0 - onon_uv.y) * iResolution.xy;
	vec4 color = vec4(0.0);
	mainImage(color, coord);
	onon_out = vec4(color.rgb, clamp(color.a, 0.0, 1.0) * clamp(0.5 - iShape(coord), 0.0, 1.0) * onon_color.a);
}
`

// Wrap is a whole fragment shader round source, a theme's shader: a
// mainImage(out vec4 color, in vec2 coord) as Shadertoy's, coord in
// pixels from the bottom left of the element's box.
func Wrap(source string) string {
	return preamble + source + epilogue
}

// Uniforms are what a shader is told of the element it draws.
type Uniforms struct {
	Width, Height float32    // the box, in pixels
	Time          float32    // seconds since onon started
	Radius        [4]float32 // corner radii in pixels, clockwise from the top left
	Scale         float32    // pixels to the logical pixel
	Alt           bool       // whether alternate-color mode is on
}

// UniformSize is the size of the block Bytes packs.
const UniformSize = 48

// Bytes packs u as the preamble's uniform block lays it out.
func (u Uniforms) Bytes() [UniformSize]byte {
	alt := float32(0)
	if u.Alt {
		alt = 1
	}
	var b [UniformSize]byte
	for i, v := range [...]float32{
		u.Width, u.Height, 1, u.Time,
		u.Radius[0], u.Radius[1], u.Radius[2], u.Radius[3],
		u.Scale, alt, 0, 0,
	} {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(v))
	}
	return b
}
