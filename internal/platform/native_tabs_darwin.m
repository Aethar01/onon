#import <Cocoa/Cocoa.h>
#import <objc/runtime.h>

// macOS gathers windows into tabs of one window. onon lets it, for windows
// that share a tabbing identifier, and answers the tab bar's + with a new
// tab of its own.

extern void ononNewWindowForTab(void);

static void ononWindowNewWindowForTab(id self, SEL _cmd, id sender) {
    (void)self;
    (void)_cmd;
    (void)sender;
    ononNewWindowForTab();
}

void ononConfigureTabbing(void *windowPointer, int enabled) {
    NSWindow *window = (__bridge NSWindow *)windowPointer;
    if (window == nil) {
        return;
    }
    window.tabbingIdentifier = @"onon.document";
    // SDL disallows tabbing for its windows; automatic follows the system's
    // "Prefer tabs when opening documents".
    window.tabbingMode = enabled ? NSWindowTabbingModeAutomatic : NSWindowTabbingModeDisallowed;
    // The tab bar shows its + only to a window that answers newWindowForTab:.
    Class windowClass = [window class];
    if (enabled && !class_respondsToSelector(windowClass, @selector(newWindowForTab:))) {
        class_addMethod(windowClass, @selector(newWindowForTab:), (IMP)ononWindowNewWindowForTab, "v@:@");
    }
}

void ononAddTabbedWindow(void *existingPointer, void *addedPointer) {
    NSWindow *existing = (__bridge NSWindow *)existingPointer;
    NSWindow *added = (__bridge NSWindow *)addedPointer;
    if (existing == nil || added == nil || existing == added) {
        return;
    }
    // A new tab goes at the end, as in other Mac apps.
    NSWindow *last = existing.tabGroup.windows.lastObject;
    if (last != nil && last != added) {
        existing = last;
    }
    [existing addTabbedWindow:added ordered:NSWindowAbove];
    added.tabGroup.selectedWindow = added; // even while onon is not active
    [added makeKeyAndOrderFront:nil];
}

// ononTabbedWindows fills out with the windows of the window's tab group, in
// tab order, up to max, and returns how many there are.
int ononTabbedWindows(void *windowPointer, void **out, int max) {
    NSWindow *window = (__bridge NSWindow *)windowPointer;
    if (window == nil) {
        return 0;
    }
    NSArray<NSWindow *> *windows = window.tabGroup.windows;
    if (windows.count == 0) {
        windows = @[ window ];
    }
    int n = 0;
    for (NSWindow *w in windows) {
        if (n < max) {
            out[n] = (__bridge void *)w;
        }
        n++;
    }
    return n;
}

void ononSelectTabbedWindow(void *windowPointer) {
    NSWindow *window = (__bridge NSWindow *)windowPointer;
    if (window == nil) {
        return;
    }
    if (window.tabGroup != nil) {
        window.tabGroup.selectedWindow = window;
    }
    [window makeKeyAndOrderFront:nil];
}

// ononMoveTabbedWindow puts the window at index among its tab group's tabs.
void ononMoveTabbedWindow(void *windowPointer, int index) {
    NSWindow *window = (__bridge NSWindow *)windowPointer;
    NSWindowTabGroup *group = window.tabGroup;
    NSArray<NSWindow *> *windows = group.windows;
    if (group == nil || index < 0 || index >= (int)windows.count || windows[index] == window) {
        return;
    }
    NSUInteger from = [windows indexOfObject:window];
    NSWindow *target = windows[index];
    [group removeWindow:window];
    [target addTabbedWindow:window ordered:(NSUInteger)index > from ? NSWindowAbove : NSWindowBelow];
    group.selectedWindow = window;
}

int ononSameTabGroup(void *aPointer, void *bPointer) {
    NSWindow *a = (__bridge NSWindow *)aPointer;
    NSWindow *b = (__bridge NSWindow *)bPointer;
    return a != nil && b != nil && a.tabGroup != nil && a.tabGroup == b.tabGroup;
}

// ononTabBarInset is how far, in points, the window's tab bar and the
// title bar above it reach down over its content, which runs under them,
// or 0 while it shows no tab bar.
double ononTabBarInset(void *windowPointer) {
    NSWindow *window = (__bridge NSWindow *)windowPointer;
    if (window == nil || window.tabGroup == nil || !window.tabGroup.isTabBarVisible) {
        return 0;
    }
    NSRect content = window.contentView.frame;
    return NSMaxY(content) - NSMaxY(window.contentLayoutRect);
}

void ononToggleTabBar(void *windowPointer) {
    NSWindow *window = (__bridge NSWindow *)windowPointer;
    if (window != nil) {
        [window toggleTabBar:nil];
    }
}

int ononTabBarVisible(void *windowPointer) {
    NSWindow *window = (__bridge NSWindow *)windowPointer;
    return window != nil && window.tabGroup != nil && window.tabGroup.isTabBarVisible ? 1 : 0;
}
