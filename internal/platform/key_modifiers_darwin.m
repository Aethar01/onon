#import <Cocoa/Cocoa.h>
#include <stdbool.h>
#include <stdint.h>

// SDL gives a key event the modifiers it has seen go down, not the ones the
// key press carries. A remapper such as Hammerspoon that turns ⌘H into a
// plain ← while ⌘ is held sends ← with no modifiers; other apps see ←, but
// SDL reports ⌘←. ononKeyFilter gives each key event the modifiers of the
// key press it came from, as other apps see them.

// The SDL 3 definitions the filter uses, from SDL_events.h and SDL_keycode.h.
// onon loads SDL at run time, so it does not build against SDL's headers.
enum {
    ononSDLKeyDown = 0x300,
    ononSDLKeyUp = 0x301,
    ononSDLKeycodeLCtrl = 0x400000e0, // the eight modifier keys run to RGui
    ononSDLKeycodeRGui = 0x400000e7,
};

typedef struct {
    uint32_t type;
    uint32_t reserved;
    uint64_t timestamp;
    uint32_t windowID;
    uint32_t which;
    uint32_t scancode;
    uint32_t key;
    uint16_t mod;
    uint16_t raw;
    bool down;
    bool repeat;
} OnonSDLKeyboardEvent;

// ononModifier is one of the four modifiers: its Cocoa flag, the
// device-dependent flags of its left and right keys, and its SDL bits.
typedef struct {
    NSEventModifierFlags flag;
    NSEventModifierFlags leftDevice, rightDevice;
    uint16_t left, right;
} OnonModifier;

static const OnonModifier ononModifiers[] = {
    {NSEventModifierFlagShift, NX_DEVICELSHIFTKEYMASK, NX_DEVICERSHIFTKEYMASK, 0x0001, 0x0002},
    {NSEventModifierFlagControl, NX_DEVICELCTLKEYMASK, NX_DEVICERCTLKEYMASK, 0x0040, 0x0080},
    {NSEventModifierFlagOption, NX_DEVICELALTKEYMASK, NX_DEVICERALTKEYMASK, 0x0100, 0x0200},
    {NSEventModifierFlagCommand, NX_DEVICELCMDKEYMASK, NX_DEVICERCMDKEYMASK, 0x0400, 0x0800},
};

// ononKeyModifiers returns mod with Shift, Control, Option and Command as
// flags has them. A modifier flags has that mod lacks takes its side from
// flags' device-dependent bits, or is the left one if they name neither.
uint16_t ononKeyModifiers(uint16_t mod, unsigned long flags) {
    for (size_t i = 0; i < sizeof ononModifiers / sizeof ononModifiers[0]; i++) {
        const OnonModifier *m = &ononModifiers[i];
        uint16_t both = m->left | m->right;
        if ((flags & m->flag) == 0) {
            mod &= ~both;
        } else if ((mod & both) == 0) {
            uint16_t side = 0;
            if (flags & m->leftDevice) {
                side |= m->left;
            }
            if (flags & m->rightDevice) {
                side |= m->right;
            }
            mod |= side != 0 ? side : m->left;
        }
    }
    return mod;
}

// ononSameKey reports whether an SDL key event with raw and down came from
// a key press or release of keyCode. SDL swaps keys 10 and 50 of ISO
// keyboards before it records them as raw.
bool ononSameKey(uint16_t raw, bool down, unsigned short keyCode, bool keyDown) {
    if (down != keyDown) {
        return false;
    }
    return raw == keyCode || ((keyCode == 10 || keyCode == 50) && raw == 60 - keyCode);
}

// ononKeyFilter is the SDL event filter. SDL turns a key press into its key
// event while AppKit dispatches the press, so the press is NSApp's current
// event. Other key events SDL makes then, such as the releases it sends when
// the window loses focus, are left as they are.
bool ononKeyFilter(void *userdata, void *sdlEvent) {
    (void)userdata;
    OnonSDLKeyboardEvent *key = sdlEvent;
    if (key->type != ononSDLKeyDown && key->type != ononSDLKeyUp) {
        return true;
    }
    if (key->key >= ononSDLKeycodeLCtrl && key->key <= ononSDLKeycodeRGui) {
        return true; // SDL makes these from flag changes; their state is right
    }
    if (![NSThread isMainThread]) {
        return true;
    }
    NSEvent *event = [NSApp currentEvent];
    NSEventType type = event.type;
    if (event == nil || (type != NSEventTypeKeyDown && type != NSEventTypeKeyUp)) {
        return true;
    }
    if (ononSameKey(key->raw, key->down, event.keyCode, type == NSEventTypeKeyDown)) {
        key->mod = ononKeyModifiers(key->mod, (unsigned long)event.modifierFlags);
    }
    return true;
}
