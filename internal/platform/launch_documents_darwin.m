#import <Cocoa/Cocoa.h>
#import <CoreServices/CoreServices.h>
#include <stdlib.h>
#include <string.h>

// Finder and `open` hand a launching app its documents in an open-documents
// Apple Event, not as arguments. SDL finishes launching NSApp before it becomes
// the app delegate, so the event can reach AppKit before SDL can turn it into a
// drop. These handlers note the launch event and its files on their way to
// AppKit's own handlers, which still run as before.

typedef struct {
    AEEventHandlerUPP handler;
    SRefCon refcon;
} OnonAppleEventHandler;

static OnonAppleEventHandler ononAppKitOpenApplication;
static OnonAppleEventHandler ononAppKitOpenDocuments;

// ononLaunchPaths collects the launch documents until they are taken.
static NSMutableArray<NSString *> *ononLaunchPaths;
static BOOL ononLaunchEventHandled;

static OSErr ononForwardAppleEvent(OnonAppleEventHandler appKit, const AppleEvent *event, AppleEvent *reply) {
    if (appKit.handler == NULL) {
        return noErr;
    }
    return InvokeAEEventHandlerUPP(event, reply, appKit.refcon, appKit.handler);
}

static void ononRecordDocuments(const AppleEvent *event) {
    AEDesc copy;
    if (AEDuplicateDesc(event, &copy) != noErr) {
        return;
    }
    @autoreleasepool {
        // The descriptor owns the copy and disposes of it when released.
        NSAppleEventDescriptor *descriptor = [[NSAppleEventDescriptor alloc] initWithAEDescNoCopy:&copy];
        NSAppleEventDescriptor *files = [[descriptor paramDescriptorForKeyword:keyDirectObject] coerceToDescriptorType:typeAEList];
        for (NSInteger i = 1; i <= files.numberOfItems; i++) {
            NSString *path = [[files descriptorAtIndex:i] fileURLValue].path;
            if (path.length > 0) {
                [ononLaunchPaths addObject:path];
            }
        }
        [descriptor release];
    }
}

static OSErr ononHandleOpenApplication(const AppleEvent *event, AppleEvent *reply, SRefCon refcon) {
    (void)refcon;
    ononLaunchEventHandled = YES;
    return ononForwardAppleEvent(ononAppKitOpenApplication, event, reply);
}

static OSErr ononHandleOpenDocuments(const AppleEvent *event, AppleEvent *reply, SRefCon refcon) {
    (void)refcon;
    if (ononLaunchPaths != nil) {
        ononRecordDocuments(event);
    }
    ononLaunchEventHandled = YES;
    return ononForwardAppleEvent(ononAppKitOpenDocuments, event, reply);
}

static void ononWrapAppleEventHandler(AEEventID eventID, AEEventHandlerProcPtr wrapper, OnonAppleEventHandler *appKit) {
    if (AEGetEventHandler(kCoreEventClass, eventID, &appKit->handler, &appKit->refcon, false) != noErr) {
        *appKit = (OnonAppleEventHandler){0};
    }
    AEInstallEventHandler(kCoreEventClass, eventID, NewAEEventHandlerUPP(wrapper), NULL, false);
}

// ononWatchLaunch must run once, before SDL creates NSApp: AppKit installs its
// handlers just before it announces it will finish launching, and dispatches
// the launch event after.
void ononWatchLaunch(void) {
    ononLaunchPaths = [[NSMutableArray alloc] init];
    @autoreleasepool {
        NSNotificationCenter *center = [NSNotificationCenter defaultCenter];
        [center addObserverForName:NSApplicationWillFinishLaunchingNotification
                            object:nil
                             queue:nil
                        usingBlock:^(NSNotification *notification) {
            (void)notification;
            ononWrapAppleEventHandler(kAEOpenApplication, ononHandleOpenApplication, &ononAppKitOpenApplication);
            ononWrapAppleEventHandler(kAEOpenDocuments, ononHandleOpenDocuments, &ononAppKitOpenDocuments);
        }];
        [center addObserverForName:NSApplicationDidFinishLaunchingNotification
                            object:nil
                             queue:nil
                        usingBlock:^(NSNotification *notification) {
            (void)notification;
            ononLaunchEventHandled = YES;
        }];
    }
}

// ononLaunchHandled reports whether the launch event has been handled, so its
// documents, if any, are known.
int ononLaunchHandled(void) {
    return ononLaunchEventHandled;
}

// ononTakeLaunchDocuments returns the launch documents and stops recording;
// later documents reach the viewer as SDL drops. The caller frees each path
// and the array.
char **ononTakeLaunchDocuments(int *count) {
    NSUInteger n = ononLaunchPaths.count;
    char **paths = n > 0 ? calloc(n, sizeof(char *)) : NULL;
    @autoreleasepool {
        for (NSUInteger i = 0; paths != NULL && i < n; i++) {
            paths[i] = strdup(ononLaunchPaths[i].fileSystemRepresentation);
        }
    }
    *count = paths != NULL ? (int)n : 0;
    [ononLaunchPaths release];
    ononLaunchPaths = nil;
    return paths;
}
