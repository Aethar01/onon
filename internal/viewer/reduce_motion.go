package viewer

import "time"

// The OS's setting for less motion, as macOS's Reduce motion or GNOME's
// animations switch, turns the theme's reduced motion on, as
// motion.force_reduced would. It is read in the background when the viewer starts and when
// its window comes forward, as reading it can mean running a program.

// osMotionReadInterval is how often the OS's setting is read at most, so
// a window focused over and over, as on a tiling desktop, does not start
// a program each time.
const osMotionReadInterval = 5 * time.Second

// readOSMotion reads the OS's setting; tests replace it.
var readOSMotion = osReducesMotion

// refreshReducedMotion reads the OS's setting again, unless it is being
// read or was read lately.
func (a *App) refreshReducedMotion() {
	m := &a.motion
	if time.Since(m.osReadAt) < osMotionReadInterval || !m.osReading.CompareAndSwap(false, true) {
		return
	}
	m.osReadAt = time.Now()
	go func() {
		defer m.osReading.Store(false)
		m.osReduced.Store(readOSMotion())
	}()
}
