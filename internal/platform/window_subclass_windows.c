#include <windows.h>
#include <windowsx.h>
#include <commctrl.h>

// A subclass of SDL's window answers the messages SDL has no use for: those
// about onon's title bar buttons and its taskbar button.
//
// onon draws its own title bar buttons, and SDL's hit test can only call
// them part of the window. Windows 11 shows its snap layouts over a window's
// maximize button only when the system is told the pointer is over one, so
// this subclass of SDL's window says so over onon's, and turns the presses
// Windows then sends as the frame's own back into the title bar's.

extern int ononTitleBarMaximizeAt(int x, int y);
extern void ononTitleBarMaximizePress(int down);
BOOL ononTaskbarMessage(HWND hwnd, UINT msg, WPARAM wParam);

static LRESULT CALLBACK ononTitleBarProc(HWND hwnd, UINT msg, WPARAM wParam, LPARAM lParam,
                                          UINT_PTR id, DWORD_PTR data) {
    (void)data;
    if (ononTaskbarMessage(hwnd, msg, wParam)) {
        return 0;
    }
    switch (msg) {
    case WM_NCHITTEST: {
        // SDL answers first, which also reveals the buttons.
        LRESULT hit = DefSubclassProc(hwnd, msg, wParam, lParam);
        if (hit == HTCLIENT) {
            POINT p = {GET_X_LPARAM(lParam), GET_Y_LPARAM(lParam)};
            if (ScreenToClient(hwnd, &p) && ononTitleBarMaximizeAt(p.x, p.y)) {
                return HTMAXBUTTON;
            }
        }
        return hit;
    }
    case WM_NCLBUTTONDOWN:
    case WM_NCLBUTTONDBLCLK:
        // Left to the system, the press would draw a classic button.
        if (wParam == HTMAXBUTTON) {
            ononTitleBarMaximizePress(1);
            return 0;
        }
        break;
    case WM_NCLBUTTONUP:
        if (wParam == HTMAXBUTTON) {
            ononTitleBarMaximizePress(0);
            return 0;
        }
        break;
    case WM_NCDESTROY:
        RemoveWindowSubclass(hwnd, ononTitleBarProc, id);
        break;
    }
    return DefSubclassProc(hwnd, msg, wParam, lParam);
}

void ononSubclassTitleBar(void *hwnd) {
    SetWindowSubclass((HWND)hwnd, ononTitleBarProc, 1, 0);
}
