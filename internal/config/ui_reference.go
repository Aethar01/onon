package config

// UIViewsReference is the reference for onon.ui's views, written into the
// documentation and its examples run by the tests.
const UIViewsReference = `Create views with ` + "`local view = onon.ui.create(spec)`" + `, then call ` + "`view:show()`" + `. A view is a menu or a panel: a list of rows, or a body its ` + "`draw`" + ` function draws, with text fields, buttons and check boxes. onon's own menus are views made the same way, so they and plugins' look and behave alike, and the theme styles both.

#### Placing a view

| Field | Meaning |
|---|---|
| ` + "`id`" + `, ` + "`title`" + ` | The view's name and the title in its header. A view with a ` + "`draw`" + ` function and no title has no header. |
| ` + "`modal`" + ` | True unless set false. A modal view is a menu: it takes the keys, closes on a click outside it, and replaces any other menu. A view that is not modal is a panel: it stays open over the document, under any menu, until closed, and takes the pointer over it, and the keys when it has them (see [Typing](#typing)). |
| ` + "`stack`" + ` | For a menu: opens it over the menu open, which closing it returns to, instead of replacing it. |
| ` + "`width`" + `, ` + "`height`" + ` | Its size: logical pixels, or a percentage of the window such as ` + "`\"30%\"`" + `; ` + "`\"70%\"`" + ` unless set, and at most the document's part of the window. A list fits its height to its rows. |
| ` + "`anchor`" + ` | Where it sits in the document's part of the window: ` + "`center`" + ` (the default), ` + "`top`" + `, ` + "`bottom`" + `, ` + "`left`" + `, ` + "`right`" + `, ` + "`top_left`" + `, ` + "`top_right`" + `, ` + "`bottom_left`" + ` or ` + "`bottom_right`" + `, a little in from the edges. With ` + "`at`" + `, the part of the view that sits at that point instead, ` + "`top_left`" + ` unless given: ` + "`bottom_left`" + ` for a popup above a point, ` + "`center`" + ` for one centred on it. |
| ` + "`at`" + ` | A point the view is pinned to: ` + "`{ x = 10, y = 40 }`" + ` in the window, in logical pixels, kept in the window, or ` + "`{ page = 3, x = 72, y = 100 }`" + ` on a page, in its points from its top left as the document events give them, moving with the page and hidden while it is not shown. |
| ` + "`dock`" + ` | ` + "`left`" + `, ` + "`right`" + `, ` + "`top`" + ` or ` + "`bottom`" + `: a panel against that side of the window, its ` + "`width`" + ` or ` + "`height`" + ` taken from the document, which lays itself out beside it. Panels docked to the same side stack inwards in the order they open. |
| ` + "`resizable`" + ` | For a docked view: its inner edge can be dragged to resize it. ` + "`view:size()`" + ` gives a view's width and height. |

#### Lists

| Field | Meaning |
|---|---|
| ` + "`rows`" + ` | The rows, each a string or a table; see below. |
| ` + "`selected`" + `, ` + "`scroll`" + `, ` + "`query`" + ` | The selected row (1-based), the first row shown, and the search query to start with. |
| ` + "`searchable`" + ` | Whether / searches the rows; true unless set false. |
| ` + "`filter`" + ` | Whether the query filters the rows; true unless set false, as for a view that sets its own rows for each query in ` + "`on_query_changed`" + `. |
| ` + "`empty`" + ` | What the list says when it has no rows. |
| ` + "`columns`" + ` | Lays out rows' ` + "`cells`" + ` as a table: a list of columns, each with a ` + "`title`" + `, a ` + "`width`" + ` (logical pixels, a percentage of the row, or ` + "`\"*\"`" + `, the default, for a share of what the others leave) and an ` + "`align`" + ` (` + "`left`" + `, ` + "`center`" + ` or ` + "`right`" + `). Titles show above the rows. |

A row table has ` + "`text`" + `, ` + "`value`" + ` (the text unless set), ` + "`id`" + `, ` + "`secondary`" + ` (drawn on the right), ` + "`depth`" + ` (indenting it), ` + "`disabled`" + `, ` + "`key`" + ` (a key binding, drawn in a column before the text), ` + "`marker`" + ` (drawn just before the text), ` + "`heading`" + ` (a section title, which cannot be chosen), ` + "`swatch`" + ` (a colour drawn before the text, written as in the elements table), ` + "`checked`" + ` (a check box before the text, checked or not), ` + "`button`" + ` (drawn as a button, as the keybind menu's new keybind is) and ` + "`cells`" + ` (its text in the view's ` + "`columns`" + `, searched as its text is). Other fields are the plugin's own, and come back with the row.

#### Drawing a view

` + "`draw = function(canvas, box, state)`" + ` draws the view's body, below its header, in place of its rows; ` + "`canvas:default()`" + ` draws the rows. The canvas has the methods element draw functions have (see [Drawing](#drawing)), and those below. ` + "`box`" + ` is the body, and ` + "`state.scroll`" + ` how far it is scrolled.

- What the function draws below the body scrolls into it, by the wheel, the keys that move a list, the scrollbar, and ` + "`view:set_scroll(pixels)`" + `.
- A function that returns true is drawn again on the next frame, for drawing that moves. Otherwise the view is drawn again after its callbacks and as the pointer moves between its regions; call ` + "`view:redraw()`" + ` when what it draws changes otherwise, as from a timer.
- If it fails, the error is reported once and the rows are drawn instead.

| Method | Draws |
|---|---|
| <code>canvas:text(x, y, text [, color] [, options])</code> | Text in ` + "`color`" + `, the foreground unless given, its options ` + "`true`" + ` for bold, or a table of ` + "`color`" + `, ` + "`bold`" + `, ` + "`mono`" + ` (a monospaced font), ` + "`size`" + ` (logical pixels), ` + "`width`" + ` (to truncate to, or with ` + "`wrap = true`" + ` wrap to) and ` + "`align`" + ` (` + "`left`" + `, ` + "`center`" + ` or ` + "`right`" + `, within the width, or about x without one). Lines break at newlines. Returns its width and height. |
| <code>canvas:measure(text [, options])</code> | Nothing; returns the width and height text would take. |
| <code>canvas:clip(x, y, w, h)</code> | Nothing; limits what is drawn after to the rect, until ` + "`canvas:clip()`" + `. |
| <code>canvas:image(path, x, y [, w [, h]])</code> | A PNG, JPEG, GIF, WebP or BMP file, a path not absolute being from the configuration's directory, at its size in logical pixels, or a width or height given, keeping its proportions if only one is. It reloads when the file changes. Returns its width and height. |
| <code>canvas:page(page, x, y, w [, h [, area]])</code> | The page, turned as the document's are: its thumbnail fitted to the width, or within the width and height, rendering as it is first drawn. With ` + "`area`" + `, ` + "`{ x =, y = [, zoom =] }`" + `, the part of the page from that point, in its points, at the box's top left, at ` + "`zoom`" + ` times the document's zoom (1 unless set), drawn sharp from the page's tiles; it needs a height. Returns its width and height. |
| <code>canvas:region(id, x, y, w, h)</code> | Nothing; names the rect for the pointer events over it, which carry the id. Returns whether the pointer is over it, for drawing it hovered. |
| <code>canvas:input(id, x, y, w [, options])</code> | The text field ` + "`id`" + `, ` + "`w`" + ` wide, with options ` + "`placeholder`" + `, ` + "`secret`" + ` (bullets for a password) and ` + "`lines`" + ` (more than one wraps and takes newlines). Returns its width and height. See [Typing](#typing). |
| <code>canvas:button(id, x, y, w, label)</code> | A button, ` + "`w`" + ` wide, clicked with the pointer, or with Space or Enter when Tab reaches it; ` + "`on_click(id)`" + ` hears of it. Returns its width and height. |
| <code>canvas:column([options])</code> | Nothing; returns a column that lays out what it draws down the canvas: see [Columns](#columns). |
| <code>canvas:checkbox(id, x, y, label, checked)</code> | A check box with its label, clicked as a button is; the plugin keeps whether it is checked, as ` + "`on_click`" + ` tells it. Returns its width and height. |

The fields, buttons and check boxes take the theme's ` + "`input`" + `, ` + "`input_focused`" + `, ` + "`button`" + `, ` + "`button_focused`" + ` and ` + "`check`" + ` elements. The cursor shows an I-beam over a field and a hand over a button or check box.

#### Columns

` + "`canvas:column({ x =, y =, width =, gap = })`" + ` lays out a form, or anything drawn down a body, without working out where each thing goes: each method draws at the column's left, below what it drew last, ` + "`gap`" + ` (8 unless given) apart, and returns its width and height. The column starts at ` + "`x`" + `, ` + "`y`" + ` (0, 0 unless given), ` + "`width`" + ` wide, as far in from the right as from the left unless given.

| Method | Draws |
|---|---|
| <code>col:text(text [, color] [, options])</code> | Text, as ` + "`canvas:text`" + ` draws it, wrapped to the column unless ` + "`wrap = false`" + `. |
| <code>col:input(id [, options])</code> | A text field the column's width. |
| <code>col:button(id, label [, width])</code> | A button, the column's width unless given. |
| <code>col:checkbox(id, label, checked)</code> | A check box. |
| <code>col:page(page [, height [, area]])</code> | A page, or part of one, the column's width. |
| <code>col:image(path [, w [, h]])</code> | An image. |
| <code>col:space(height)</code> | Nothing; moves down. |
| <code>col:y()</code> | Nothing; returns where the next thing goes. |

Everything a canvas draws out of sight, below or above what shows of the body, is neither drawn nor, for a page, rendered, so a view can draw all it holds, as every page of a long document, and costs only what shows.

#### Typing

- ` + "`prompt = \"Open\"`" + ` puts a text field in the header, labelled with it, whose text is the query: typing filters a list without /, and Enter chooses the selected row, or runs ` + "`on_submit`" + ` if the view has one.
- ` + "`canvas:input`" + ` draws a text field. The first drawn has the typing until Tab, Shift-Tab or a click moves it; Tab and Shift-Tab move through the prompt, fields, buttons and check boxes in the order they are drawn. ` + "`view:field(id)`" + ` and ` + "`view:set_field(id, text)`" + ` read and write a field's text, before the view opens too, and ` + "`view:fields()`" + ` gives them all, as an ` + "`on_submit`" + ` reading a form wants.
- Fields and prompts edit as onon's own prompt does, with the same keys, a press placing the cursor, a drag selecting, and copy, cut and paste with the clipboard actions. Text an input method is composing shows underlined at the cursor. In a field of more than one line, Enter starts a new line, Ctrl- or Cmd-Enter submits, and the arrows, Home and End move by line.
- A menu has the keys while it is open. A panel takes them when clicked, if it has fields, buttons, check boxes, a prompt or ` + "`on_key`" + `, or from ` + "`view:focus()`" + `, until Escape, a click elsewhere, or ` + "`view:blur()`" + `; ` + "`view:focused()`" + ` says whether it has them.

#### Callbacks

| Callback | Runs |
|---|---|
| ` + "`on_select(index, value, text, id, row)`" + ` | When a row is chosen. ` + "`index`" + ` and ` + "`id`" + ` are the row's in the rows as set, however filtered, and ` + "`row`" + ` is the row as given, string or table. |
| ` + "`on_selection_changed(index, value, text, id, row)`" + ` | When a key, the pointer or the query moves the selection, not when the view opens or ` + "`set_selected`" + ` moves it, as for a live preview. |
| ` + "`on_query_changed(query)`" + ` | When the user changes the search query or prompt, not when ` + "`set_query`" + ` does. |
| ` + "`on_input(text, id)`" + ` | When the user changes a field's text. |
| ` + "`on_submit(text, id)`" + ` | On Enter in a field, or in the prompt, whose ` + "`id`" + ` is nil. |
| ` + "`on_click(id)`" + ` | When a button or check box is clicked. |
| ` + "`on_key(key)`" + ` | For each key pressed while the view has the keys and is not being typed in, named as ` + "`bind`" + ` names it, before the view's own keys; returning true takes the key. |
| ` + "`on_action(action)`" + ` | For the action bound to a key pressed while the view has the keys, before the view runs it; returning true takes the action. |
| ` + "`on_mouse(event)`" + ` | For the pointer over the view, and after a press on it until the release; returning true keeps the view from handling it. ` + "`event.type`" + ` is ` + "`press`" + `, ` + "`release`" + `, ` + "`move`" + `, ` + "`leave`" + ` or ` + "`wheel`" + `; ` + "`event.x`" + ` and ` + "`event.y`" + ` are logical pixels from the top left of what the body draws, scrolled; ` + "`event.region`" + ` is the id of the region there; a press or release has ` + "`button`" + ` (` + "`left`" + `, ` + "`middle`" + `, ` + "`right`" + `, ` + "`x1`" + ` or ` + "`x2`" + `) and a press ` + "`clicks`" + `; a wheel has ` + "`dx`" + ` and ` + "`dy`" + `, positive to the right and down. A press on a text field, button or check box goes to it instead. |
| ` + "`on_close()`" + ` | When the view closes: by ` + "`view:close()`" + ` or, for a menu, the user, another menu replacing it, or the document closing. |

View methods are ` + "`show()`" + `, ` + "`close()`" + `, ` + "`visible()`" + `, ` + "`redraw()`" + `, ` + "`size()`" + `, ` + "`focus()`" + `, ` + "`blur()`" + `, ` + "`focused()`" + `, ` + "`set_title(title)`" + `, ` + "`set_rows(rows)`" + `, ` + "`set_selected(index)`" + `, ` + "`set_scroll(scroll)`" + `, ` + "`set_query(query)`" + `, ` + "`field(id)`" + `, ` + "`fields()`" + `, ` + "`set_field(id, text)`" + `, ` + "`selected()`" + `, ` + "`scroll()`" + `, and ` + "`query()`" + `. Selection indices are 1-based, with 0 meaning no selection. A drawn body's scroll is in logical pixels, a list's in rows. Once a view closes, its methods read the state it closed with, as ` + "`on_close`" + ` does, until it is shown again from its spec.

` + "```lua" + `
-- A sidebar of page thumbnails, docked left and resizable; clicking one
-- goes to it.
local sidebar = onon.ui.create({
  dock = "left", width = 144, resizable = true,
  draw = function(canvas, box)
    local size, y = box.w - 24, 8
    for page = 1, onon.page_count() do
      local _, h = canvas:page(page, 12, y, size)
      if canvas:region(page, 12, y, size, h) or page == onon.page() then
        canvas:stroke(canvas:path():rect(11, y - 1, size + 2, h + 2), "accent", 2)
      end
      y = y + h + 12
    end
  end,
  on_mouse = function(ev)
    if ev.type == "press" and ev.region then onon.page(ev.region) return true end
  end,
})
sidebar:show()

-- A form over the document, laid out by a column.
local form
form = onon.ui.create({
  title = "New bookmark", width = 360, height = 260,
  draw = function(canvas, box)
    local col = canvas:column({ x = 12, y = 8 })
    col:text("Name", "muted")
    col:input("name", { placeholder = "Chapter 1" })
    col:input("note", { placeholder = "Note", lines = 3 })
    col:button("save", "Save")
  end,
  on_click = function(id)
    if id == "save" then
      local fields = form:fields()
      onon.message("saved " .. fields.name)
      form:close()
    end
  end,
})
form:show()
` + "```" + `
`
