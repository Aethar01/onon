package shader

import "errors"

func translate(spirv []uint32, format Format) ([]byte, string, error) {
	if format != FormatMSL {
		return nil, "", errors.New("the GPU takes shaders in a form macOS has no translator for")
	}
	msl, err := cross(spirv, false)
	if err != nil {
		return nil, "", err
	}
	return append([]byte(msl), 0), "main0", nil
}
