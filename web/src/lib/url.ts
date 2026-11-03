// siteAddress turns what someone typed for a site into the form browsers
// use, with internationalised names in ASCII (punycode).
export function siteAddress(s: string) {
  const t = s.trim()
  try {
    return new URL(/^[a-z][a-z0-9+.-]*:\/\//i.test(t) ? t : `https://${t}`).hostname || t
  } catch {
    return t
  }
}

export function hostOf(u: string) {
  try {
    return new URL(u).host.replace(/^www\./, '')
  } catch {
    return u
  }
}
