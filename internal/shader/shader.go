// Package shader turns the GLSL fragment shaders themes are written with
// into what a GPU takes: SPIR-V, by glslang, which Vulkan takes as it is,
// and from that Metal's shading language or Direct3D's bytecode, by
// SPIRV-Cross, on the systems that have them.
package shader

/*
#cgo LDFLAGS: -lglslang -lglslang-default-resource-limits

#include <stdlib.h>
#include <string.h>
#include <glslang/Include/glslang_c_interface.h>
#include <glslang/Public/resource_limits_c.h>

// onon_spirv compiles a GLSL fragment shader for Vulkan. It returns the
// SPIR-V's words, to be freed, or NULL with the compiler's log, to be freed.
static unsigned int *onon_spirv(const char *source, size_t *count, char **log) {
	glslang_input_t input = {
		.language = GLSLANG_SOURCE_GLSL,
		.stage = GLSLANG_STAGE_FRAGMENT,
		.client = GLSLANG_CLIENT_VULKAN,
		.client_version = GLSLANG_TARGET_VULKAN_1_0,
		.target_language = GLSLANG_TARGET_SPV,
		.target_language_version = GLSLANG_TARGET_SPV_1_0,
		.code = source,
		.default_version = 450,
		.default_profile = GLSLANG_NO_PROFILE,
		.messages = GLSLANG_MSG_DEFAULT_BIT,
		.resource = glslang_default_resource(),
	};
	unsigned int *words = NULL;
	glslang_program_t *program = NULL;
	glslang_shader_t *shader = glslang_shader_create(&input);
	if (!glslang_shader_preprocess(shader, &input) || !glslang_shader_parse(shader, &input)) {
		*log = strdup(glslang_shader_get_info_log(shader));
		goto done;
	}
	program = glslang_program_create();
	glslang_program_add_shader(program, shader);
	if (!glslang_program_link(program, GLSLANG_MSG_SPV_RULES_BIT | GLSLANG_MSG_VULKAN_RULES_BIT)) {
		*log = strdup(glslang_program_get_info_log(program));
		goto done;
	}
	glslang_program_SPIRV_generate(program, GLSLANG_STAGE_FRAGMENT);
	*count = glslang_program_SPIRV_get_size(program);
	words = malloc(*count * sizeof(unsigned int));
	glslang_program_SPIRV_get(program, words);
done:
	if (program) glslang_program_delete(program);
	glslang_shader_delete(shader);
	return words;
}
*/
import "C"

import (
	"errors"
	"strings"
	"sync"
	"unsafe"
)

var start = sync.OnceValue(func() bool { return C.glslang_initialize_process() != 0 })

// SPIRV compiles source, a GLSL fragment shader written for Vulkan, as
// Wrap writes one.
func SPIRV(source string) ([]uint32, error) {
	if !start() {
		return nil, errors.New("cannot start glslang")
	}
	src := C.CString(source)
	defer C.free(unsafe.Pointer(src))
	var count C.size_t
	var log *C.char
	words := C.onon_spirv(src, &count, &log)
	if words == nil {
		return nil, logError(log)
	}
	defer C.free(unsafe.Pointer(words))
	return append([]uint32(nil), unsafe.Slice((*uint32)(unsafe.Pointer(words)), int(count))...), nil
}

// logError is glslang's log as an error.
func logError(log *C.char) error {
	if log == nil {
		return errors.New("shader failed to compile")
	}
	defer C.free(unsafe.Pointer(log))
	return compilerError(C.GoString(log))
}

// compilerError is a compiler's log as an error, without the lines that
// say nothing of the fault.
func compilerError(log string) error {
	var lines []string
	for line := range strings.SplitSeq(log, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasSuffix(line, "compilation errors.  No code generated.") {
			continue
		}
		lines = append(lines, strings.TrimPrefix(line, "ERROR: "))
	}
	if len(lines) == 0 {
		return errors.New("shader failed to compile")
	}
	return errors.New(strings.Join(lines, "; "))
}
