#import <Cocoa/Cocoa.h>

// The menu bar is built from the menus the Go side describes in JSON. Items
// with a tag run the viewer's action of that tag; the others are the
// system's own, such as Hide and Minimize.

extern void ononMenuBarChose(int tag);
extern int ononMenuBarItemState(int tag);

static NSString *ononApplicationName(void) {
    NSDictionary *info = [[NSBundle mainBundle] infoDictionary];
    NSString *name = info[@"CFBundleDisplayName"];
    if (name.length == 0) {
        name = info[@"CFBundleName"];
    }
    if (name.length == 0) {
        name = [[NSProcessInfo processInfo] processName];
    }
    return name;
}

// ononItemMatches reports whether a key press is an item's key equivalent.
static BOOL ononItemMatches(NSMenuItem *item, NSEvent *event) {
    NSString *key = item.keyEquivalent;
    if (key.length == 0) {
        return NO;
    }
    NSEventModifierFlags mask = NSEventModifierFlagCommand | NSEventModifierFlagOption |
                                NSEventModifierFlagControl | NSEventModifierFlagShift;
    NSEventModifierFlags want = item.keyEquivalentModifierMask & mask;
    if (![key.lowercaseString isEqualToString:key]) {
        want |= NSEventModifierFlagShift;
    }
    return (event.modifierFlags & mask) == want &&
           [event.charactersIgnoringModifiers.lowercaseString isEqualToString:key.lowercaseString];
}

@interface OnonMenuBarTarget : NSObject <NSMenuDelegate>
@end

@implementation OnonMenuBarTarget

// menuNeedsUpdate: shows each item as the viewer is now, as the menu opens.
- (void)menuNeedsUpdate:(NSMenu *)menu {
    for (NSMenuItem *item in menu.itemArray) {
        if (item.target == self) {
            int state = ononMenuBarItemState((int)item.tag);
            item.enabled = (state & 1) != 0;
            item.state = (state & 2) != 0 ? NSControlStateValueOn : NSControlStateValueOff;
        }
    }
}

// SDL hands every key press to the viewer, which runs the action bound to
// it, so an item chosen by its own key equivalent has run already.
- (void)choose:(NSMenuItem *)item {
    NSEvent *event = NSApp.currentEvent;
    if (event.type == NSEventTypeKeyDown && ononItemMatches(item, event)) {
        return;
    }
    ononMenuBarChose((int)item.tag);
}

@end

static OnonMenuBarTarget *ononMenuBarTarget;

// OnonMainMenu is the menu bar. SDL hands every key press to the viewer,
// which runs the action bound to it, so a key equivalent of one of the
// viewer's items is claimed here without the menu running it again or
// flashing its title: while such a key is held, the flash holds back its
// repeats, and a held key's action runs only when it is let go.
@interface OnonMainMenu : NSMenu
@end

@implementation OnonMainMenu

static BOOL ononMenuHasActionKey(NSMenu *menu, NSEvent *event) {
    for (NSMenuItem *item in menu.itemArray) {
        if (item.submenu != nil && ononMenuHasActionKey(item.submenu, event)) {
            return YES;
        }
        if (item.target == ononMenuBarTarget && ononItemMatches(item, event)) {
            return YES;
        }
    }
    return NO;
}

- (BOOL)performKeyEquivalent:(NSEvent *)event {
    if (event.type == NSEventTypeKeyDown && ononMenuHasActionKey(self, event)) {
        return YES;
    }
    return [super performKeyEquivalent:event];
}

@end

// OnonRecentDocuments fills Open Recent with the documents macOS keeps
// for onon each time it opens. NSDocumentController fills it only for
// document-based applications. A document chosen goes to SDL's application
// delegate, as one opened from Finder does, which hands it to the viewer
// as a drop.
@interface OnonRecentDocuments : NSObject <NSMenuDelegate>
@end

@implementation OnonRecentDocuments

- (void)menuNeedsUpdate:(NSMenu *)menu {
    [menu removeAllItems];
    NSDocumentController *documents = [NSDocumentController sharedDocumentController];
    for (NSURL *url in documents.recentDocumentURLs) {
        NSMenuItem *item = [menu addItemWithTitle:url.lastPathComponent action:@selector(open:) keyEquivalent:@""];
        item.target = self;
        item.representedObject = url;
        item.image = [[NSWorkspace sharedWorkspace] iconForFile:url.path];
        item.image.size = NSMakeSize(16, 16);
    }
    if (menu.numberOfItems > 0) {
        [menu addItem:[NSMenuItem separatorItem]];
    }
    NSMenuItem *clear = [menu addItemWithTitle:@"Clear Menu" action:@selector(clear:) keyEquivalent:@""];
    clear.target = self;
    clear.enabled = documents.recentDocumentURLs.count > 0;
}

- (void)open:(NSMenuItem *)item {
    NSURL *url = item.representedObject;
    if ([NSApp.delegate respondsToSelector:@selector(application:openFile:)]) {
        [NSApp.delegate application:NSApp openFile:url.path];
    }
}

- (void)clear:(NSMenuItem *)item {
    (void)item;
    [[NSDocumentController sharedDocumentController] clearRecentDocuments:nil];
}

@end

static OnonRecentDocuments *ononRecentDocuments;

static NSMenuItem *ononSystemItem(NSString *role, NSString *title, NSMenu **servicesMenu) {
    SEL action = NULL;
    id target = nil;
    NSString *key = @"";
    NSEventModifierFlags modifiers = NSEventModifierFlagCommand;
    if ([role isEqualToString:@"about"]) {
        action = @selector(orderFrontStandardAboutPanel:);
        target = NSApp;
    } else if ([role isEqualToString:@"hide"]) {
        action = @selector(hide:);
        target = NSApp;
        key = @"h";
    } else if ([role isEqualToString:@"hide_others"]) {
        action = @selector(hideOtherApplications:);
        target = NSApp;
        key = @"h";
        modifiers |= NSEventModifierFlagOption;
    } else if ([role isEqualToString:@"show_all"]) {
        action = @selector(unhideAllApplications:);
        target = NSApp;
    } else if ([role isEqualToString:@"minimize"]) {
        action = @selector(miniaturize:);
        key = @"m";
    } else if ([role isEqualToString:@"zoom"]) {
        action = @selector(zoom:);
    } else if ([role isEqualToString:@"front"]) {
        action = @selector(arrangeInFront:);
        target = NSApp;
    } else if ([role isEqualToString:@"merge_windows"]) {
        action = @selector(mergeAllWindows:); // to the key window, as each acts on its own
    } else if ([role isEqualToString:@"move_tab_to_window"]) {
        action = @selector(moveTabToNewWindow:);
    } else if ([role isEqualToString:@"tab_overview"]) {
        action = @selector(toggleTabOverview:);
        key = @"\\";
        modifiers |= NSEventModifierFlagShift;
    } else if ([role isEqualToString:@"tab_bar"]) {
        action = @selector(toggleTabBar:);
    } else if (![role isEqualToString:@"services"] && ![role isEqualToString:@"recent_documents"]) {
        return nil; // a role macOS has no item for
    }
    NSMenuItem *item = [[[NSMenuItem alloc] initWithTitle:title action:action keyEquivalent:key] autorelease];
    item.keyEquivalentModifierMask = modifiers;
    item.target = target;
    if ([role isEqualToString:@"services"]) {
        *servicesMenu = [[[NSMenu alloc] initWithTitle:title] autorelease];
        item.submenu = *servicesMenu;
    } else if ([role isEqualToString:@"recent_documents"]) {
        NSMenu *recent = [[[NSMenu alloc] initWithTitle:title] autorelease];
        recent.delegate = ononRecentDocuments;
        recent.autoenablesItems = NO;
        item.submenu = recent;
    }
    return item;
}

static NSMenuItem *ononActionItem(NSDictionary *spec, NSString *title) {
    NSString *key = spec[@"key"] ?: @"";
    NSMenuItem *item = [[[NSMenuItem alloc] initWithTitle:title action:@selector(choose:) keyEquivalent:key] autorelease];
    NSEventModifierFlags modifiers = 0;
    if ([spec[@"cmd"] boolValue]) {
        modifiers |= NSEventModifierFlagCommand;
    }
    if ([spec[@"ctrl"] boolValue]) {
        modifiers |= NSEventModifierFlagControl;
    }
    if ([spec[@"alt"] boolValue]) {
        modifiers |= NSEventModifierFlagOption;
    }
    if ([spec[@"shift"] boolValue]) {
        modifiers |= NSEventModifierFlagShift;
    }
    item.keyEquivalentModifierMask = modifiers;
    item.target = ononMenuBarTarget;
    item.tag = [spec[@"tag"] integerValue];
    return item;
}

static void ononBuildMenuBar(NSData *data) {
    NSArray *menus = [NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
    if (![menus isKindOfClass:[NSArray class]]) {
        return;
    }
    if (ononMenuBarTarget == nil) {
        ononMenuBarTarget = [[OnonMenuBarTarget alloc] init];
        ononRecentDocuments = [[OnonRecentDocuments alloc] init];
    }
    NSString *appName = ononApplicationName();
    NSMenu *mainMenu = [[[OnonMainMenu alloc] init] autorelease];
    NSMenu *servicesMenu = nil;
    NSMenu *windowsMenu = nil;
    NSMenu *helpMenu = nil;
    for (NSDictionary *spec in menus) {
        NSString *title = [spec[@"title"] stringByReplacingOccurrencesOfString:@"{app}" withString:appName];
        NSMenu *menu = [[[NSMenu alloc] initWithTitle:title] autorelease];
        menu.autoenablesItems = NO;
        menu.delegate = ononMenuBarTarget;
        for (NSDictionary *itemSpec in spec[@"items"]) {
            NSString *itemTitle = [itemSpec[@"title"] ?: @"" stringByReplacingOccurrencesOfString:@"{app}" withString:appName];
            NSMenuItem *item;
            if ([itemSpec[@"tag"] integerValue] > 0) {
                item = ononActionItem(itemSpec, itemTitle);
            } else if (itemSpec[@"role"] != nil) {
                item = ononSystemItem(itemSpec[@"role"], itemTitle, &servicesMenu);
            } else {
                item = [NSMenuItem separatorItem];
            }
            if (item != nil) {
                [menu addItem:item];
            }
        }
        NSMenuItem *holder = [[[NSMenuItem alloc] initWithTitle:title action:NULL keyEquivalent:@""] autorelease];
        holder.submenu = menu;
        [mainMenu addItem:holder];
        if ([spec[@"role"] isEqualToString:@"window"]) {
            windowsMenu = menu;
        } else if ([spec[@"role"] isEqualToString:@"help"]) {
            helpMenu = menu;
        }
    }
    NSApp.mainMenu = mainMenu;
    NSApp.servicesMenu = servicesMenu;
    NSApp.windowsMenu = windowsMenu;
    NSApp.helpMenu = helpMenu;
}

void ononSetMenuBar(const char *menus) {
    NSData *data = [[NSData alloc] initWithBytes:menus length:strlen(menus)];
    if ([NSThread isMainThread]) {
        @autoreleasepool {
            ononBuildMenuBar(data);
        }
        [data release];
    } else {
        dispatch_async(dispatch_get_main_queue(), ^{
            @autoreleasepool {
                ononBuildMenuBar(data);
            }
            [data release];
        });
    }
}
