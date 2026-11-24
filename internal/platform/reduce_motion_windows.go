//go:build windows

package platform

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var procSystemParametersInfo = windows.NewLazySystemDLL("user32.dll").NewProc("SystemParametersInfoW")

const spiGetClientAreaAnimation = 0x1042 // SPI_GETCLIENTAREAANIMATION

// ReducesMotion reports whether Windows' "Animation effects" setting is off.
func ReducesMotion() bool {
	var animate int32
	ok, _, _ := procSystemParametersInfo.Call(spiGetClientAreaAnimation, 0, uintptr(unsafe.Pointer(&animate)), 0)
	return ok != 0 && animate == 0
}
