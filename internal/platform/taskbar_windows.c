#define COBJMACROS
#include <windows.h>
#include <commctrl.h>
#include <shobjidl.h>

// The taskbar's thumbnail of a onon window carries Previous Page and Next
// Page buttons, as media players' carry theirs. They can be added only once
// the taskbar has made the window's button, which it announces with a
// registered message; the window's subclass passes that and the buttons'
// clicks here.

extern void ononTaskbarButton(int id);

enum { ononThumbPrevious = 1, ononThumbNext = 2 };

static UINT ononTaskbarCreated;

// ononSystemUsesLightTheme reports whether the taskbar, and with it the
// thumbnail's buttons, are light.
static BOOL ononSystemUsesLightTheme(void) {
    DWORD light = 0, size = sizeof(light);
    RegGetValueW(HKEY_CURRENT_USER, L"Software\\Microsoft\\Windows\\CurrentVersion\\Themes\\Personalize",
                 L"SystemUsesLightTheme", RRF_RT_REG_DWORD, NULL, &light, &size);
    return light != 0;
}

// ononArrowIcon draws a small icon of a triangle pointing left or right,
// smoothed by sampling each pixel four by four.
static HICON ononArrowIcon(BOOL right, BOOL dark) {
    int size = GetSystemMetrics(SM_CXSMICON);
    BITMAPV5HEADER header = {0};
    header.bV5Size = sizeof(header);
    header.bV5Width = size;
    header.bV5Height = -size; // top-down
    header.bV5Planes = 1;
    header.bV5BitCount = 32;
    header.bV5Compression = BI_BITFIELDS;
    header.bV5RedMask = 0x00FF0000;
    header.bV5GreenMask = 0x0000FF00;
    header.bV5BlueMask = 0x000000FF;
    header.bV5AlphaMask = 0xFF000000;
    void *bits = NULL;
    HDC dc = GetDC(NULL);
    HBITMAP color = CreateDIBSection(dc, (BITMAPINFO *)&header, DIB_RGB_COLORS, &bits, NULL, 0);
    ReleaseDC(NULL, dc);
    if (color == NULL) {
        return NULL;
    }
    DWORD rgb = dark ? 0x202020 : 0xFFFFFF;
    DWORD *pixels = bits;
    // The triangle's tip and the middle of its base, in pixels.
    double tip = size * (right ? 0.72 : 0.28), base = size * (right ? 0.32 : 0.68);
    double half = size * 0.3, middle = size / 2.0;
    for (int y = 0; y < size; y++) {
        for (int x = 0; x < size; x++) {
            int covered = 0;
            for (int sy = 0; sy < 4; sy++) {
                for (int sx = 0; sx < 4; sx++) {
                    double px = x + (sx + 0.5) / 4, py = y + (sy + 0.5) / 4;
                    double along = (px - base) / (tip - base); // 0 at the base, 1 at the tip
                    if (along >= 0 && along <= 1 && py >= middle - half * (1 - along) &&
                        py <= middle + half * (1 - along)) {
                        covered++;
                    }
                }
            }
            DWORD alpha = covered * 255 / 16;
            pixels[y * size + x] = (alpha << 24) | rgb;
        }
    }
    HBITMAP mask = CreateBitmap(size, size, 1, 1, NULL);
    ICONINFO info = {TRUE, 0, 0, mask, color};
    HICON icon = CreateIconIndirect(&info);
    DeleteObject(mask);
    DeleteObject(color);
    return icon;
}

static void ononAddThumbButtons(HWND hwnd) {
    // SDL may have started COM on this thread already, which is fine.
    HRESULT com = CoInitializeEx(NULL, COINIT_APARTMENTTHREADED);
    ITaskbarList3 *taskbar = NULL;
    if (FAILED(CoCreateInstance(&CLSID_TaskbarList, NULL, CLSCTX_INPROC_SERVER, &IID_ITaskbarList3,
                                (void **)&taskbar))) {
        if (SUCCEEDED(com)) {
            CoUninitialize();
        }
        return;
    }
    if (SUCCEEDED(ITaskbarList3_HrInit(taskbar))) {
        BOOL dark = ononSystemUsesLightTheme();
        THUMBBUTTON buttons[2] = {0};
        buttons[0].dwMask = THB_ICON | THB_TOOLTIP | THB_FLAGS;
        buttons[0].iId = ononThumbPrevious;
        buttons[0].hIcon = ononArrowIcon(FALSE, dark);
        wcscpy_s(buttons[0].szTip, ARRAYSIZE(buttons[0].szTip), L"Previous Page");
        buttons[0].dwFlags = THBF_ENABLED;
        buttons[1] = buttons[0];
        buttons[1].iId = ononThumbNext;
        buttons[1].hIcon = ononArrowIcon(TRUE, dark);
        wcscpy_s(buttons[1].szTip, ARRAYSIZE(buttons[1].szTip), L"Next Page");
        ITaskbarList3_ThumbBarAddButtons(taskbar, hwnd, ARRAYSIZE(buttons), buttons);
        // The taskbar keeps copies of the icons.
        DestroyIcon(buttons[0].hIcon);
        DestroyIcon(buttons[1].hIcon);
    }
    ITaskbarList3_Release(taskbar);
    if (SUCCEEDED(com)) {
        CoUninitialize();
    }
}

// ononTaskbarMessage handles the window messages about its taskbar
// button, reporting whether msg was one.
BOOL ononTaskbarMessage(HWND hwnd, UINT msg, WPARAM wParam) {
    if (ononTaskbarCreated == 0) {
        ononTaskbarCreated = RegisterWindowMessageW(L"TaskbarButtonCreated");
    }
    if (msg == ononTaskbarCreated && msg != 0) {
        ononAddThumbButtons(hwnd);
        return TRUE;
    }
    if (msg == WM_COMMAND && HIWORD(wParam) == THBN_CLICKED) {
        ononTaskbarButton(LOWORD(wParam));
        return TRUE;
    }
    return FALSE;
}
