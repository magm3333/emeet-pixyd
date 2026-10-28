package pixy

import "slices"

// Route limits. Mirrors the preset limits in shape (named, capped
// collection) but routes are capped more tightly on step count since each
// step is a full position+speed snapshot rather than a single value.
const (
	// MaxRoutes is the maximum number of named motion routes that can be stored.
	MaxRoutes = 16
	// MaxRouteNameLength is the maximum length of a route name in characters.
	MaxRouteNameLength = 32
	// MaxRouteSteps bounds a single recording so an accidentally long
	// capture (e.g. a slider left dragging) cannot grow state.json
	// unboundedly. Recording stops accepting new steps past this cap; the
	// steps already captured are kept.
	MaxRouteSteps = 500
)

// ValidateRouteName reports whether name is an acceptable route label. It
// reuses the exact rules of ValidatePresetName (non-empty after trimming,
// length bound, no path separators or control characters), classified
// through the same ErrInvalidPresetName sentinel so the two name spaces
// share one validation contract and one HTTP/CLI error shape.
func ValidateRouteName(name string) error {
	return ValidatePresetName(name)
}

// RouteStep is one recorded point in a motion route ("Modo recorrido"): the
// absolute pan/tilt/zoom position, the motor speed in effect when the step
// was captured, and how long to wait — from the previous step, or from
// playback start for the first step of a lap — before applying it.
type RouteStep struct {
	Pan    int         `json:"pan"`
	Tilt   int         `json:"tilt"`
	Zoom   int         `json:"zoom"`
	Speeds SpeedValues `json:"speeds,omitzero"`
	// WaitMs is the recorded gap before this step, in milliseconds. Ignored
	// for the first step of each playback lap, which is applied immediately
	// (playback always starts by moving straight to the route's starting
	// position).
	WaitMs int64 `json:"waitMs"`
}

// PTZ returns the step's position as a PTZValues.
func (s RouteStep) PTZ() PTZValues {
	return PTZValues{Pan: s.Pan, Tilt: s.Tilt, Zoom: s.Zoom}
}

// Route is a named, ordered sequence of recorded PTZ movements ("Modo
// recorrido"). Playback replays every step at its recorded speed and wait
// time; see RoutePlayOptions for loop/ping-pong semantics.
type Route struct {
	Steps []RouteStep `json:"steps"`
}

// RouteMap is a named collection of saved motion routes. It serializes as a
// plain JSON object (map), matching PresetMap's on-disk shape.
type RouteMap map[string]Route

// NewRouteMap returns an empty, initialized RouteMap.
func NewRouteMap() RouteMap { return make(RouteMap) }

// SortedNames returns the route names sorted alphabetically.
func (r RouteMap) SortedNames() []string {
	names := make([]string, 0, len(r))
	for name := range r {
		names = append(names, name)
	}

	slices.Sort(names)

	return names
}

// Get returns the route for name, or false if not found.
func (r RouteMap) Get(name string) (Route, bool) {
	v, ok := r[name]

	return v, ok
}

// IsFull reports whether the route collection has reached MaxRoutes.
func (r RouteMap) IsFull() bool { return len(r) >= MaxRoutes }
