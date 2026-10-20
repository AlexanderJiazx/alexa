package browser

// indexJS marks visible, unoccluded interactive elements in the viewport and
// returns a compact description of each plus scroll info and visible text.
const indexJS = `(() => {
  const MAXTXT = 90, MAXEL = 250;
  const vw = innerWidth, vh = innerHeight;
  const SEL = 'a[href],button,input:not([type=hidden]),select,textarea,summary,[role=button],[role=link],[role=checkbox],[role=radio],[role=tab],[role=menuitem],[role=menuitemcheckbox],[role=menuitemradio],[role=option],[role=switch],[role=combobox],[role=textbox],[role=searchbox],[role=slider],[role=spinbutton],[role=treeitem],[role=gridcell],[contenteditable=""],[contenteditable=true],[onclick],[tabindex]:not([tabindex="-1"]),label[for]';
  const els = [], out = [];
  let above = 0, below = 0, visited = 0;
  const clean = s => (s || '').replace(/\s+/g, ' ').trim();
  const cut = (s, n) => s.length > n ? s.slice(0, n - 1) + '…' : s;
  function visible(el, r) {
    if (r.width < 3 || r.height < 3) return false;
    const st = getComputedStyle(el);
    if (st.visibility === 'hidden' || st.display === 'none' || parseFloat(st.opacity) < 0.05) return false;
    return true;
  }
  function onTop(el, r, root) {
    const pts = [[r.left + r.width / 2, r.top + r.height / 2], [r.left + 3, r.top + 3], [r.right - 3, r.bottom - 3]];
    for (const [x0, y0] of pts) {
      const x = Math.min(Math.max(x0, 0), vw - 1), y = Math.min(Math.max(y0, 0), vh - 1);
      let top = (root.elementFromPoint ? root : document).elementFromPoint(x, y);
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
      if (type === 'password') text = v ? 'value=••••' : 'value=""';
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
  function consider(el, root) {
    if (els.length >= MAXEL) return;
    const r = el.getBoundingClientRect();
    if (r.bottom < 0) { if (r.width > 2) above++; return; }
    if (r.top > vh) { if (r.width > 2) below++; return; }
    if (r.right < 0 || r.left > vw) return;
    if (!visible(el, r)) return;
    if (!onTop(el, r, root)) return;
    els.push(el);
    out.push({ d: describe(el), x: Math.max(0, r.left), y: Math.max(0, r.top), w: Math.min(r.width, vw - Math.max(0, r.left)), h: Math.min(r.height, vh - Math.max(0, r.top)) });
  }
  const pointerParent = new WeakSet();
  function walk(root) {
    const tw = document.createTreeWalker(root, NodeFilter.SHOW_ELEMENT);
    let n = tw.currentNode.nodeType === 1 ? tw.currentNode : tw.nextNode();
    while (n && visited < 25000) {
      visited++;
      if (n.matches && n.matches(SEL)) {
        consider(n, root);
      } else if (n.tagName && /^(DIV|SPAN|LI|IMG|SVG|TD|I|P|H[1-6]|ARTICLE|SECTION)$/i.test(n.tagName)) {
        const par = n.parentElement;
        if (!(par && pointerParent.has(par)) && !n.closest(SEL)) {
          const st = getComputedStyle(n);
          if (st.cursor === 'pointer') { pointerParent.add(n); consider(n, root); }
        } else if (par && pointerParent.has(par)) pointerParent.add(n);
      }
      if (n.shadowRoot) walk(n.shadowRoot);
      n = tw.nextNode();
    }
  }
  walk(document.documentElement);
  window.__aaEls = [null].concat(els);
  // Visible text excerpt.
  let text = '', tlen = 0;
  const tw2 = document.createTreeWalker(document.body || document.documentElement, NodeFilter.SHOW_TEXT);
  const range = document.createRange();
  let tn;
  while ((tn = tw2.nextNode()) && tlen < 2500) {
    const s = clean(tn.textContent);
    if (!s) continue;
    const pe = tn.parentElement;
    if (!pe || /^(SCRIPT|STYLE|NOSCRIPT|TEMPLATE)$/.test(pe.tagName)) continue;
    range.selectNodeContents(tn);
    const r = range.getBoundingClientRect();
    if (r.bottom < 0 || r.top > vh || r.width === 0) continue;
    text += s + ' '; tlen += s.length + 1;
  }
  const se = document.scrollingElement || document.documentElement;
  return { els: out, above, below, scrollY: Math.round(se.scrollTop), scrollH: Math.round(se.scrollHeight), vh, vw, text: cut(text.trim(), 2500), title: document.title, url: location.href };
})()`
