#define COBJMACROS
#include <windows.h>
#include <shobjidl.h>
#include <propsys.h>

// The jump list, the menu of onon's taskbar button and Start entry, lists
// the documents opened lately and an Open File… task, which starts onon
// showing its file picker.

// PKEY_Title, the name a jump list shows for a link.
static const PROPERTYKEY ononTitleKey = {
    {0xF29F85E0, 0x4FF9, 0x1068, {0xAB, 0x91, 0x08, 0x00, 0x2B, 0x27, 0xB3, 0xD9}}, 2};

static IShellLinkW *ononTaskLink(const wchar_t *exe, const wchar_t *title, const wchar_t *arguments) {
    IShellLinkW *link = NULL;
    if (FAILED(CoCreateInstance(&CLSID_ShellLink, NULL, CLSCTX_INPROC_SERVER, &IID_IShellLinkW, (void **)&link))) {
        return NULL;
    }
    IShellLinkW_SetPath(link, exe);
    IShellLinkW_SetArguments(link, arguments);
    IShellLinkW_SetIconLocation(link, exe, 0);
    IShellLinkW_SetDescription(link, title);
    IPropertyStore *properties = NULL;
    if (SUCCEEDED(IShellLinkW_QueryInterface(link, &IID_IPropertyStore, (void **)&properties))) {
        PROPVARIANT value;
        PropVariantInit(&value);
        value.vt = VT_LPWSTR;
        value.pwszVal = (wchar_t *)title; // copied by SetValue
        IPropertyStore_SetValue(properties, &ononTitleKey, &value);
        IPropertyStore_Commit(properties);
        IPropertyStore_Release(properties);
    }
    return link;
}

// ononSetJumpList writes the jump list for the app with the given
// AppUserModelID.
void ononSetJumpList(const char *appID) {
    wchar_t id[128];
    if (MultiByteToWideChar(CP_UTF8, 0, appID, -1, id, ARRAYSIZE(id)) == 0) {
        return;
    }
    wchar_t exe[MAX_PATH];
    DWORD length = GetModuleFileNameW(NULL, exe, ARRAYSIZE(exe));
    if (length == 0 || length == ARRAYSIZE(exe)) {
        return;
    }
    HRESULT com = CoInitializeEx(NULL, COINIT_APARTMENTTHREADED);
    ICustomDestinationList *list = NULL;
    IObjectCollection *tasks = NULL;
    IObjectArray *removed = NULL, *taskArray = NULL;
    UINT slots = 0;
    if (FAILED(CoCreateInstance(&CLSID_DestinationList, NULL, CLSCTX_INPROC_SERVER, &IID_ICustomDestinationList,
                                (void **)&list))) {
        goto done;
    }
    ICustomDestinationList_SetAppID(list, id);
    if (FAILED(ICustomDestinationList_BeginList(list, &slots, &IID_IObjectArray, (void **)&removed))) {
        goto done;
    }
    // A list of onon's own replaces the one Windows keeps, so it names the
    // recent documents for Windows to fill in.
    ICustomDestinationList_AppendKnownCategory(list, KDC_RECENT);
    if (SUCCEEDED(CoCreateInstance(&CLSID_EnumerableObjectCollection, NULL, CLSCTX_INPROC_SERVER,
                                   &IID_IObjectCollection, (void **)&tasks))) {
        IShellLinkW *open = ononTaskLink(exe, L"Open File\x2026", L"--command open_file_picker");
        if (open != NULL) {
            IObjectCollection_AddObject(tasks, (IUnknown *)open);
            IShellLinkW_Release(open);
        }
        if (SUCCEEDED(IObjectCollection_QueryInterface(tasks, &IID_IObjectArray, (void **)&taskArray))) {
            ICustomDestinationList_AddUserTasks(list, taskArray);
        }
    }
    ICustomDestinationList_CommitList(list);

done:
    if (taskArray != NULL) {
        IObjectArray_Release(taskArray);
    }
    if (tasks != NULL) {
        IObjectCollection_Release(tasks);
    }
    if (removed != NULL) {
        IObjectArray_Release(removed);
    }
    if (list != NULL) {
        ICustomDestinationList_Release(list);
    }
    if (SUCCEEDED(com)) {
        CoUninitialize();
    }
}
