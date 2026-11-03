// Speaker says who said a message, for screen readers: the bubbles only
// show it by where they sit and how they look.
export function Speaker({ who }: { who: 'you' | 'alexa' }) {
  return <span className="sr">{who === 'you' ? 'You said: ' : 'Alexa: '}</span>
}
