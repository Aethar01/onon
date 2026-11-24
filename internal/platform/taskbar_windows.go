//go:build windows

package platform

/*
#cgo LDFLAGS: -lole32 -luuid -lgdi32
#include <stdlib.h>
void ononSetJumpList(const char *appID);
*/
import "C"

import "unsafe"

func setJumpList() {
	id := C.CString(appUserModelID)
	defer C.free(unsafe.Pointer(id))
	C.ononSetJumpList(id)
}

var taskbarButtons = []string{"prev_page", "next_page"}

//export ononTaskbarButton
func ononTaskbarButton(id C.int) {
	if i := int(id) - 1; i >= 0 && i < len(taskbarButtons) && hooks.OnAction != nil {
		hooks.OnAction(taskbarButtons[i])
	}
}
