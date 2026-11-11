//go:build !darwin && !windows

package shader

import "errors"

// translate has nothing to translate to: Vulkan takes SPIR-V as it is.
func translate([]uint32, Format) ([]byte, string, error) {
	return nil, "", errors.New("the GPU takes shaders in a form this system has no translator for")
}
