#import <Cocoa/Cocoa.h>
#import <objc/runtime.h>

static const void *OnonTitlebarHoverControllerKey = &OnonTitlebarHoverControllerKey;
static const CGFloat ononFadedTrafficLightAlpha = 0.01;

static void ononApplyMacOSWindowStyleProperties(NSWindow *window) {
    if (window == nil) {
        return;
    }

    window.titleVisibility = NSWindowTitleHidden;
    window.titlebarAppearsTransparent = YES;

    if ((window.styleMask & NSWindowStyleMaskFullSizeContentView) == 0) {
        NSRect frame = window.frame;
        window.styleMask |= NSWindowStyleMaskFullSizeContentView;
        [window setFrame:frame display:NO];
    }
}

@interface OnonTitlebarDragView : NSView
@end

@implementation OnonTitlebarDragView

- (BOOL)isOpaque {
    return NO;
}

- (void)drawRect:(NSRect)dirtyRect {
    (void)dirtyRect;
}

- (void)mouseDown:(NSEvent *)event {
    [self.window performWindowDragWithEvent:event];
}

@end

@interface OnonTitlebarHoverController : NSObject {
    NSWindow *_window;
    NSView *_trackingView;
    NSTrackingArea *_trackingArea;
    OnonTitlebarDragView *_dragView;
    NSButton *_buttons[3];
    BOOL _visible;
}

- (instancetype)initWithWindow:(NSWindow *)window;

@end

@implementation OnonTitlebarHoverController

- (instancetype)initWithWindow:(NSWindow *)window {
    self = [super init];
    if (self == nil) {
        return nil;
    }

    _window = window;
    if (![self rebuildTitlebarViews]) {
        [self release];
        return nil;
    }

    [[NSNotificationCenter defaultCenter]
        addObserver:self
        selector:@selector(windowDidExitFullScreen:)
        name:NSWindowDidExitFullScreenNotification
        object:window];

    return self;
}

- (void)tearDownTitlebarViews {
    if (_trackingArea != nil && _trackingView != nil) {
        [_trackingView removeTrackingArea:_trackingArea];
    }

    [_dragView removeFromSuperview];
    [_dragView release];
    _dragView = nil;

    [_trackingArea release];
    _trackingArea = nil;

    [_trackingView release];
    _trackingView = nil;

    for (NSUInteger i = 0; i < 3; i++) {
        [_buttons[i] release];
        _buttons[i] = nil;
    }
}

- (BOOL)rebuildTitlebarViews {
    [self tearDownTitlebarViews];

    _buttons[0] = [[_window standardWindowButton:NSWindowCloseButton] retain];
    _buttons[1] = [[_window standardWindowButton:NSWindowMiniaturizeButton] retain];
    _buttons[2] = [[_window standardWindowButton:NSWindowZoomButton] retain];

    if (_buttons[0] == nil || _buttons[1] == nil || _buttons[2] == nil) {
        [self tearDownTitlebarViews];
        return NO;
    }

    _trackingView = [[_buttons[0] superview] retain];
    if (_trackingView == nil) {
        [self tearDownTitlebarViews];
        return NO;
    }

    _dragView = [[OnonTitlebarDragView alloc] initWithFrame:_trackingView.bounds];
    _dragView.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
    [_trackingView addSubview:_dragView positioned:NSWindowBelow relativeTo:nil];

    _trackingArea = [[NSTrackingArea alloc]
        initWithRect:NSZeroRect
        options:(NSTrackingMouseEnteredAndExited |
                 NSTrackingActiveAlways |
                 NSTrackingInVisibleRect)
        owner:self
        userInfo:nil];
    [_trackingView addTrackingArea:_trackingArea];

    [self setTrafficLightsVisible:NO animated:NO];
    return YES;
}

// setTrafficLightsVisible fades the buttons almost out rather than hiding
// them: macOS reports a fully transparent button as disabled, so tools that
// close windows through Accessibility, such as Hammerspoon, could not press
// it, and AppKit shows hidden buttons again when it lays out the titlebar.
// At ononFadedTrafficLightAlpha they cannot be seen.
- (void)setTrafficLightsVisible:(BOOL)visible animated:(BOOL)animated {
    if (_visible == visible && animated) {
        return;
    }
    _visible = visible;

    CGFloat alpha = visible ? 1.0 : ononFadedTrafficLightAlpha;
    if (!animated) {
        for (NSUInteger i = 0; i < 3; i++) {
            _buttons[i].alphaValue = alpha;
        }
        return;
    }

    [NSAnimationContext runAnimationGroup:^(NSAnimationContext *context) {
        context.duration = 0.16;
        context.allowsImplicitAnimation = YES;

        for (NSUInteger i = 0; i < 3; i++) {
            _buttons[i].animator.alphaValue = alpha;
        }
    } completionHandler:nil];
}

- (void)mouseEntered:(NSEvent *)event {
    (void)event;
    [self setTrafficLightsVisible:YES animated:YES];
}

- (void)mouseExited:(NSEvent *)event {
    (void)event;
    [self setTrafficLightsVisible:NO animated:YES];
}

- (void)windowDidExitFullScreen:(NSNotification *)notification {
    (void)notification;

    ononApplyMacOSWindowStyleProperties(_window);
    [self rebuildTitlebarViews];
}

- (void)dealloc {
    [[NSNotificationCenter defaultCenter] removeObserver:self];
    [self tearDownTitlebarViews];
    [super dealloc];
}

@end

static void ononApplyMacOSWindowStyle(NSWindow *window) {
    if (window == nil) {
        return;
    }

    ononApplyMacOSWindowStyleProperties(window);

    OnonTitlebarHoverController *controller =
        [[OnonTitlebarHoverController alloc] initWithWindow:window];
    if (controller != nil) {
        objc_setAssociatedObject(
            window,
            OnonTitlebarHoverControllerKey,
            controller,
            OBJC_ASSOCIATION_RETAIN_NONATOMIC
        );
        [controller release];
    }
}

void ononConfigureMacOSWindow(void *windowPointer) {
    NSWindow *window = (__bridge NSWindow *)windowPointer;
    if (window == nil) {
        return;
    }

    if ([NSThread isMainThread]) {
        ononApplyMacOSWindowStyle(window);
    } else {
        dispatch_async(dispatch_get_main_queue(), ^{
            ononApplyMacOSWindowStyle(window);
        });
    }
}

int ononReduceMotion(void) {
    return [[NSWorkspace sharedWorkspace] accessibilityDisplayShouldReduceMotion] ? 1 : 0;
}

static void ononApplyWindowDocument(NSWindow *window, NSString *path, BOOL edited) {
    window.representedURL = path.length > 0 ? [NSURL fileURLWithPath:path] : nil;
    window.documentEdited = edited;
}

void ononSetWindowDocument(void *windowPointer, const char *path, int edited) {
    NSWindow *window = (__bridge NSWindow *)windowPointer;
    if (window == nil) {
        return;
    }
    NSString *document = [[NSString alloc] initWithUTF8String:path];
    if ([NSThread isMainThread]) {
        ononApplyWindowDocument(window, document, edited != 0);
        [document release];
    } else {
        dispatch_async(dispatch_get_main_queue(), ^{
            ononApplyWindowDocument(window, document, edited != 0);
            [document release];
        });
    }
}
