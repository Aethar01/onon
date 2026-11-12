package shader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const frosted = `
void mainImage(out vec4 fragColor, in vec2 fragCoord) {
	vec2 uv = fragCoord / iResolution.xy;
	fragColor = texture(iChannel0, uv) * (0.9 + 0.1 * sin(iTime));
}
`

func TestWrappedShaderCompilesToSPIRV(t *testing.T) {
	spirv, err := SPIRV(Wrap(frosted))
	if err != nil {
		t.Fatal(err)
	}
	code, entry, err := Translate(spirv, FormatSPIRV)
	if err != nil {
		t.Fatal(err)
	}
	// SPIR-V starts with its magic number.
	if len(code) != 4*len(spirv) || [4]byte(code[:4]) != [4]byte{0x03, 0x02, 0x23, 0x07} || entry != "main" {
		t.Errorf("SPIR-V of %d bytes starting % x, entry %q", len(code), code[:4], entry)
	}
}

func TestShaderErrorNamesTheThemesLine(t *testing.T) {
	_, err := SPIRV(Wrap("void mainImage(out vec4 c, in vec2 p) {\n\tc = vec4(nope);\n}\n"))
	if err == nil {
		t.Fatal("no error for an undeclared name")
	}
	if !strings.Contains(err.Error(), "0:2:") || !strings.Contains(err.Error(), "nope") {
		t.Errorf("error %q does not name line 2 and the name", err)
	}
}

func TestUniformsPackAsTheBlock(t *testing.T) {
	b := Uniforms{Width: 1, Alt: true}.Bytes()
	if got := [4]byte(b[0:4]); got != [4]byte{0, 0, 0x80, 0x3f} {
		t.Errorf("width packed as %v", got)
	}
	if got := [4]byte(b[36:40]); got != [4]byte{0, 0, 0x80, 0x3f} {
		t.Errorf("alt packed as %v", got)
	}
}

func TestReflectSeesWhatAShaderReads(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         Usage
	}{
		{"time and backdrop", frosted, Usage{Time: true, Backdrop: true}},
		{"neither", "void mainImage(out vec4 c, in vec2 p) { c = vec4(p / iResolution.xy, 0.0, 1.0); }", Usage{}},
		{"iTime only in a comment", "// iTime\nvoid mainImage(out vec4 c, in vec2 p) { c = vec4(iScale); }", Usage{}},
		{"time", "void mainImage(out vec4 c, in vec2 p) { c = vec4(sin(iTime)); }", Usage{Time: true}},
		{"backdrop through a function", "vec4 at(sampler2D s, vec2 p) { return texture(s, p); }\nvoid mainImage(out vec4 c, in vec2 p) { c = at(iChannel0, p / iResolution.xy); }", Usage{Backdrop: true}},
		{"texelFetch", "void mainImage(out vec4 c, in vec2 p) { c = texelFetch(iChannel0, ivec2(p), 0); }", Usage{Backdrop: true}},
	} {
		spirv, err := SPIRV(Wrap(tc.source))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := Reflect(spirv); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestCompiledProgramsAreCached(t *testing.T) {
	dir := t.TempDir()
	defer func(old func() string) { CacheDir = old }(CacheDir)
	CacheDir = func() string { return dir }
	first, err := Compile(frosted, FormatSPIRV)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("%d files cached, want 1", len(entries))
	}
	// What is cached is what is used: spoil the entry's code and see it.
	path := filepath.Join(dir, entries[0].Name())
	data, _ := os.ReadFile(path)
	data[len(data)-1] ^= 0xff
	os.WriteFile(path, data, 0o644)
	second, err := Compile(frosted, FormatSPIRV)
	if err != nil {
		t.Fatal(err)
	}
	if second.Entry != first.Entry || second.Usage != first.Usage || len(second.Code) != len(first.Code) || second.Code[len(second.Code)-1] == first.Code[len(first.Code)-1] {
		t.Error("the second compile did not come from the cache")
	}
	if cacheKey("1/2", "x", FormatSPIRV) == cacheKey("1/3", "x", FormatSPIRV) {
		t.Error("another build of onon finds this one's programs")
	}
	if cacheKey("", "x", FormatSPIRV) != "" {
		t.Error("programs are cached when the build cannot be told")
	}
	if _, err := Compile("void mainImage(out vec4 c, in vec2 p) { c = vec4(nope); }", FormatSPIRV); err == nil {
		t.Error("no error for a shader that does not compile")
	}
}
