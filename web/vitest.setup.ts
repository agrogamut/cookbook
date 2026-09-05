import "@testing-library/jest-dom/vitest";

// jsdom implements no ResizeObserver, and react-resizable-panels constructs one the moment a
// ResizablePanelGroup mounts -- so without this any test rendering a split-pane screen dies
// with "n is not a constructor" from inside the library, nowhere near the component at fault.
//
// A no-op is the honest stub rather than a limitation: jsdom performs no layout, so every
// element measures zero and a faithful ResizeObserver would only ever report zero-sized
// entries. Nothing these tests assert depends on a panel's measured width; they assert what is
// on the screen and what the API was called with. A component that genuinely depended on the
// observed size would need a real browser, not a better fake.
if (!("ResizeObserver" in globalThis)) {
  globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
}

// jsdom lays nothing out, so getBoundingClientRect returns an all-zero rect for every element
// -- and react-resizable-panels hit-tests a document-level pointerdown against its drag
// separators' rects. Every element sitting at (0, 0, 0, 0) means every separator's hit region
// contains the (0, 0) point user-event dispatches at, so a click on any field inside a
// ResizablePanel is read as a click on the drag handle: the separator takes focus, the field
// never does, and typing goes nowhere. The failure is silent -- the click event still fires,
// only the focus lands elsewhere -- which makes it read as "the form ignored my input".
//
// Moving the shared origin off (0, 0) is enough to miss: the geometry stays exactly as
// degenerate as jsdom always made it, so nothing that worked before changes, and the pointer
// no longer falls inside a hit region that only contained it by virtue of everything being
// at the origin. Real browsers compute real rects and never had this problem, which is why it
// appeared the moment a split-pane layout was tested and not when one shipped.
const OFFSCREEN = 10_000;
Element.prototype.getBoundingClientRect = function getBoundingClientRect(): DOMRect {
  return {
    x: OFFSCREEN, y: OFFSCREEN, width: 0, height: 0,
    top: OFFSCREEN, right: OFFSCREEN, bottom: OFFSCREEN, left: OFFSCREEN,
    toJSON() { return this; },
  } as DOMRect;
};
