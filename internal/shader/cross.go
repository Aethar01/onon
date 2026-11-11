//go:build darwin || windows

package shader

/*
#include <stdlib.h>
#include <string.h>
#include <spirv_cross/spirv_cross_c.h>

// onon_cross translates SPIR-V to Metal's shading language, or with hlsl
// to Direct3D's, its texture and uniforms where SDL's GPU renderer binds
// them. It returns the source, to be freed, or NULL with the error, to be
// freed.
static char *onon_cross(const unsigned int *words, size_t count, int hlsl, char **log) {
	char *out = NULL;
	spvc_context context = NULL;
	spvc_parsed_ir ir = NULL;
	spvc_compiler compiler = NULL;
	spvc_compiler_options options = NULL;
	const char *result = NULL;
	if (spvc_context_create(&context) != SPVC_SUCCESS) {
		*log = strdup("cannot start SPIRV-Cross");
		return NULL;
	}
	if (spvc_context_parse_spirv(context, words, count, &ir) != SPVC_SUCCESS ||
		spvc_context_create_compiler(context, hlsl ? SPVC_BACKEND_HLSL : SPVC_BACKEND_MSL, ir, SPVC_CAPTURE_MODE_TAKE_OWNERSHIP, &compiler) != SPVC_SUCCESS)
		goto fail;
	if (!hlsl) {
		// SDL keeps a fragment shader's textures in set 2 and its uniforms in
		// set 3, which Metal numbers from 0 each. Direct3D keeps the sets,
		// as register spaces.
		spvc_msl_resource_binding_2 binding;
		spvc_msl_resource_binding_init_2(&binding);
		binding.stage = SpvExecutionModelFragment;
		binding.desc_set = 2;
		binding.binding = 0;
		binding.count = 1;
		binding.msl_texture = 0;
		binding.msl_sampler = 0;
		if (spvc_compiler_msl_add_resource_binding_2(compiler, &binding) != SPVC_SUCCESS)
			goto fail;
		spvc_msl_resource_binding_init_2(&binding);
		binding.stage = SpvExecutionModelFragment;
		binding.desc_set = 3;
		binding.binding = 0;
		binding.count = 1;
		binding.msl_buffer = 0;
		if (spvc_compiler_msl_add_resource_binding_2(compiler, &binding) != SPVC_SUCCESS)
			goto fail;
	}
	if (spvc_compiler_create_compiler_options(compiler, &options) != SPVC_SUCCESS ||
		spvc_compiler_options_set_uint(options, hlsl ? SPVC_COMPILER_OPTION_HLSL_SHADER_MODEL : SPVC_COMPILER_OPTION_MSL_VERSION,
			hlsl ? 51 : SPVC_MAKE_MSL_VERSION(2, 1, 0)) != SPVC_SUCCESS ||
		spvc_compiler_install_compiler_options(compiler, options) != SPVC_SUCCESS ||
		spvc_compiler_compile(compiler, &result) != SPVC_SUCCESS)
		goto fail;
	out = strdup(result);
	spvc_context_destroy(context);
	return out;
fail:
	*log = strdup(spvc_context_get_last_error_string(context));
	spvc_context_destroy(context);
	return NULL;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

// cross translates a fragment shader's SPIR-V to the source of Metal's
// shading language, or with hlsl of Direct3D's.
func cross(spirv []uint32, hlsl bool) (string, error) {
	var log *C.char
	var toHLSL C.int
	if hlsl {
		toHLSL = 1
	}
	out := C.onon_cross((*C.uint)(unsafe.Pointer(&spirv[0])), C.size_t(len(spirv)), toHLSL, &log)
	if out == nil {
		if log == nil {
			return "", errors.New("the shader could not be translated")
		}
		defer C.free(unsafe.Pointer(log))
		return "", errors.New(C.GoString(log))
	}
	defer C.free(unsafe.Pointer(out))
	return C.GoString(out), nil
}
