// cx joins class names, skipping the empty ones: cx('btn', primary && 'primary').
export function cx(...names: (string | false | null | undefined)[]) {
  return names.filter(Boolean).join(' ')
}
