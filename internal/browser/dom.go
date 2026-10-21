package browser

// indexFn marks visible, unoccluded interactive elements in view and returns
// a compact description of each plus scroll info and visible text. It goes
// into same-origin frames itself; cross-origin frames can't be reached from
// the page, so it lists them (window.__aaXFrames) for the Go side to index
// through their own targets, running this same function inside them with
// opts.frame set and opts.clip limited to the part of the frame in view.
// Boxes are in the coordinates of the window it runs in.
const indexFn = `(opts) => {
  const MAXTXT = 90, MAXEL = opts.maxEl ?? 250, MAXTEXT = opts.maxText ?? 2500;
  const SEL = 'a,button,input:not([type=hidden]),select,textarea,summary,[role=button],[role=link],[role=checkbox],[role=radio],[role=tab],[role=menuitem],[role=menuitemcheckbox],[role=menuitemradio],[role=option],[role=switch],[role=combobox],[role=textbox],[role=searchbox],[role=slider],[role=spinbutton],[role=treeitem],[role=gridcell],[contenteditable=""],[contenteditable=true],[onclick],[tabindex]:not([tabindex="-1"]),label[for],[draggable=true]';
  const els = [], out = [], frames = [], xframes = [], xout = [];
  let above = 0, below = 0, visited = 0;
  const clean = s => (s || '').replace(/\s+/g, ' ').trim();
  const cut = (s, n) => s.length > n ? s.slice(0, n - 1) + '…' : s;
  const top = { doc: document, win: window, ox: 0, oy: 0, vw: innerWidth, vh: innerHeight, label: '', counts: !opts.frame,
    clip: opts.clip || { l: 0, t: 0, r: innerWidth, b: innerHeight } };
  function visible(el, r, fx) {
    if (r.width < 3 || r.height < 3) return false;
    const st = fx.win.getComputedStyle(el);
    if (st.visibility === 'hidden' || st.display === 'none' || parseFloat(st.opacity) < 0.05) return false;
    return true;
  }
  function onTop(el, r, root, fx) {
    const pts = [[r.left + r.width / 2, r.top + r.height / 2], [r.left + 3, r.top + 3], [r.right - 3, r.bottom - 3]];
    for (const [x0, y0] of pts) {
      const x = Math.min(Math.max(x0, 0), fx.vw - 1), y = Math.min(Math.max(y0, 0), fx.vh - 1);
      let top = (root.elementFromPoint ? root : fx.doc).elementFromPoint(x, y);
      while (top && top.shadowRoot && top.shadowRoot.elementFromPoint) {
        const inner = top.shadowRoot.elementFromPoint(x, y);
        if (!inner || inner === top) break;
        top = inner;
      }
      if (!top) continue;
      if (top === el || el.contains(top) || top.contains(el)) return true;
      const lab = top.closest && top.closest('label');
      if (lab && (lab.control === el || lab.contains(el))) return true;
    }
    return false;
  }
  function describe(el) {
    const tag = el.tagName.toLowerCase();
    const a = [];
    const role = el.getAttribute('role');
    if (role) a.push('role=' + role);
    let type = '';
    if (tag === 'input') { type = (el.type || 'text').toLowerCase(); a.push('type=' + type); }
    const name = el.getAttribute('name'); if (name && (tag === 'input' || tag === 'select' || tag === 'textarea')) a.push('name=' + cut(name, 30));
    const ph = el.getAttribute('placeholder'); if (ph) a.push('placeholder="' + cut(clean(ph), 40) + '"');
    const al = el.getAttribute('aria-label'); if (al) a.push('aria-label="' + cut(clean(al), 60) + '"');
    const ti = el.getAttribute('title'); if (ti && !al) a.push('title="' + cut(clean(ti), 50) + '"');
    if (tag === 'a') { const h = el.getAttribute('href') || ''; if (h && !h.startsWith('javascript')) a.push('href="' + cut(h, 70) + '"'); }
    if (type === 'range') { a.push('min=' + (el.min || 0)); a.push('max=' + (el.max || 100)); if (el.step && el.step !== '1') a.push('step=' + el.step); }
    if (el.disabled) a.push('disabled');
    if (el.getAttribute('aria-expanded')) a.push('expanded=' + el.getAttribute('aria-expanded'));
    if (el.getAttribute('aria-selected') === 'true') a.push('selected');
    if (type === 'checkbox' || type === 'radio' || role === 'checkbox' || role === 'radio' || role === 'switch') {
      const c = el.checked !== undefined ? el.checked : el.getAttribute('aria-checked') === 'true';
      a.push(c ? 'checked' : 'unchecked');
    }
    let text = '';
    if (tag === 'input' || tag === 'textarea') {
      const v = el.value || '';
      // A password typed in from a saved login stays masked even if the
      // page turns its field into a text field.
      if (type === 'password' || el.__aaSecret) text = v ? 'value=••••' : 'value=""';
      else if (!['checkbox', 'radio', 'submit', 'button', 'file'].includes(type)) text = 'value="' + cut(clean(v), 60) + '"';
      else if (type === 'submit' || type === 'button') text = cut(clean(v), MAXTXT);
    } else if (tag === 'select') {
      const opts = Array.from(el.options).slice(0, 15).map(o => (o.selected ? '*' : '') + cut(clean(o.text), 30));
      text = 'options: ' + opts.join(' | ') + (el.options.length > 15 ? ' | …' : '');
    } else {
      text = cut(clean(el.innerText || el.textContent || ''), MAXTXT);
      if (!text) { const img = el.querySelector && el.querySelector('img[alt]'); if (img) text = 'img: ' + cut(clean(img.alt), 50); }
      if (!text && tag === 'img') text = 'img: ' + cut(clean(el.alt || ''), 50);
    }
    return '<' + tag + (a.length ? ' ' + a.join(' ') : '') + '>' + (text ? ' ' + text : '');
  }
  function consider(el, root, fx) {
    if (els.length >= MAXEL) return;
    const r = el.getBoundingClientRect();
    const ax = r.left + fx.ox, ay = r.top + fx.oy, c = fx.clip;
    if (ay + r.height < c.t) { if (fx.counts && r.width > 2) above++; return; }
    if (ay > c.b) { if (fx.counts && r.width > 2) below++; return; }
    if (ax + r.width < c.l || ax > c.r) return;
    if (!visible(el, r, fx)) return;
    if (!onTop(el, r, root, fx)) return;
    els.push(el);
    const x = Math.max(c.l, ax), y = Math.max(c.t, ay);
    out.push({ d: describe(el) + fx.label, x, y, w: Math.min(ax + r.width, c.r) - x, h: Math.min(ay + r.height, c.b) - y });
  }
  // A frame's content box in this window's coordinates.
  function frameBox(fr, fx) {
    const r = fr.getBoundingClientRect(), st = fx.win.getComputedStyle(fr), px = k => parseFloat(st[k]) || 0;
    const x = fx.ox + r.left + fr.clientLeft + px('paddingLeft'), y = fx.oy + r.top + fr.clientTop + px('paddingTop');
    return { r, x, y, w: fr.clientWidth - px('paddingLeft') - px('paddingRight'), h: fr.clientHeight - px('paddingTop') - px('paddingBottom') };
  }
  function enterFrame(fr, root, fx) {
    if (frames.length + xframes.length >= 24) return;
    const b = frameBox(fr, fx), c = fx.clip;
    const clip = { l: Math.max(c.l, b.x), t: Math.max(c.t, b.y), r: Math.min(c.r, b.x + b.w), b: Math.min(c.b, b.y + b.h) };
    if (b.w < 16 || b.h < 16 || clip.r - clip.l < 8 || clip.b - clip.t < 8) return;
    if (!visible(fr, b.r, fx) || !onTop(fr, b.r, root, fx)) return;
    let doc = null;
    try { doc = fr.contentDocument; } catch (e) {}
    let host = '';
    try { host = new URL(fr.src, location.href).host; } catch (e) {}
    const name = cut(clean(fr.title || fr.name || ''), 40) || host || 'embedded';
    if (doc && doc.documentElement) {
      const w = doc.defaultView;
      const sub = { doc, win: w, ox: b.x, oy: b.y, vw: w.innerWidth, vh: w.innerHeight, clip, counts: false, label: ' (in frame "' + name + '")', name };
      frames.push(sub);
      walk(doc.documentElement, sub);
    } else {
      xframes.push(fr);
      xout.push({ x: b.x, y: b.y, w: b.w, h: b.h, clip, name, src: cut(fr.src || '', 120) });
    }
  }
  const pointerParent = new WeakSet();
  function walk(root, fx) {
    const tw = fx.doc.createTreeWalker(root, NodeFilter.SHOW_ELEMENT);
    let n = tw.currentNode.nodeType === 1 ? tw.currentNode : tw.nextNode();
    while (n && visited < 25000) {
      visited++;
      if (n.tagName === 'IFRAME' || n.tagName === 'FRAME') {
        enterFrame(n, root, fx);
      } else if (n.matches && n.matches(SEL)) {
        consider(n, root, fx);
      } else if (n.tagName && /^(DIV|SPAN|LI|IMG|SVG|TD|I|P|H[1-6]|ARTICLE|SECTION)$/i.test(n.tagName)) {
        const par = n.parentElement;
        if (!(par && pointerParent.has(par)) && !n.closest(SEL)) {
          const st = fx.win.getComputedStyle(n);
          if (st.cursor === 'pointer') { pointerParent.add(n); consider(n, root, fx); }
        } else if (par && pointerParent.has(par)) pointerParent.add(n);
      }
      if (n.shadowRoot) walk(n.shadowRoot, fx);
      n = tw.nextNode();
    }
  }
  walk(document.documentElement, top);
  window.__aaEls = [null].concat(els);
  window.__aaXFrames = xframes;
  // Where an element is, in this window's coordinates, through same-origin frames.
  window.__aaAbs = (el) => {
    const r = el.getBoundingClientRect();
    let x = r.left, y = r.top, w = el.ownerDocument.defaultView;
    while (w && w !== window && w.frameElement) {
      const fe = w.frameElement, fr = fe.getBoundingClientRect(), st = fe.ownerDocument.defaultView.getComputedStyle(fe);
      x += fr.left + fe.clientLeft + (parseFloat(st.paddingLeft) || 0);
      y += fr.top + fe.clientTop + (parseFloat(st.paddingTop) || 0);
      w = w.parent;
    }
    return { x, y, w: r.width, h: r.height };
  };
  // Visible text excerpt, this window first, then any same-origin frames.
  let text = '', tlen = 0;
  function texts(fx, prefix) {
    const body = fx.doc.body || fx.doc.documentElement;
    if (!body) return;
    const tw2 = fx.doc.createTreeWalker(body, NodeFilter.SHOW_TEXT);
    const range = fx.doc.createRange(), c = fx.clip;
    let tn, first = true;
    while ((tn = tw2.nextNode()) && tlen < MAXTEXT) {
      const s = clean(tn.textContent);
      if (!s) continue;
      const pe = tn.parentElement;
      if (!pe || /^(SCRIPT|STYLE|NOSCRIPT|TEMPLATE)$/.test(pe.tagName)) continue;
      range.selectNodeContents(tn);
      const r = range.getBoundingClientRect();
      const ax = r.left + fx.ox, ay = r.top + fx.oy;
      if (r.width === 0 || ay + r.height < c.t || ay > c.b || ax + r.width < c.l || ax > c.r) continue;
      if (first && prefix) { text += prefix; tlen += prefix.length; }
      first = false;
      text += s + ' '; tlen += s.length + 1;
    }
  }
  texts(top, '');
  for (const f of frames) texts(f, '[In frame "' + f.name + '":] ');
  const se = document.scrollingElement || document.documentElement;
  return { els: out, above, below, scrollY: Math.round(se.scrollTop), scrollH: Math.round(se.scrollHeight), vh: innerHeight, vw: innerWidth,
    text: cut(text.trim(), MAXTEXT), title: document.title, url: location.href, xframes: xout };
}`

// readFn returns the page's HTML for browser_read, with scripts and the like
// removed and same-origin frames inlined. With opts.frames, cross-origin
// frames that are visible become [[AA-XFRAME-n]] markers, n being their
// position in window.__aaReadXF, for the Go side to fill in.
const readFn = `(opts) => {
  const xf = [], xsrc = [];
  function html(doc, depth) {
    const root = doc.body || doc.documentElement;
    const c = root.cloneNode(true);
    const live = Array.from(root.querySelectorAll('iframe,frame'));
    Array.from(c.querySelectorAll('iframe,frame')).forEach((f, i) => {
      const fr = live[i];
      let d = null;
      try { d = fr && fr.contentDocument; } catch (e) {}
      const r = fr ? fr.getBoundingClientRect() : { width: 0, height: 0 };
      const big = r.width >= 40 && r.height >= 40;
      const name = fr ? (fr.title || fr.name || fr.src || '') : '';
      if (d && d.documentElement && depth < 4) {
        const sec = doc.createElement('section');
        sec.innerHTML = '<p><em>[Embedded frame' + (name ? ': ' + name.replace(/[<>]/g, '') : '') + ']</em></p>' + html(d, depth + 1);
        f.replaceWith(sec);
      } else if (fr && big && depth === 0 && opts.frames && xf.length < 10) {
        xf.push(fr); xsrc.push(fr.src || '');
        const ph = doc.createElement('div');
        ph.textContent = '[[AA-XFRAME-' + (xf.length - 1) + ']]';
        f.replaceWith(ph);
      } else {
        f.remove();
      }
    });
    c.querySelectorAll('script,style,noscript,svg,template,link,meta').forEach(e => e.remove());
    return c.innerHTML;
  }
  const out = html(document, 0);
  window.__aaReadXF = xf;
  return { html: out, xsrc };
}`
