import type { Block } from '../../api/blocks'
import { photos } from './demo'

// Block fixtures: every fixture of SYNTHESIS Appendix A.6 (mirrored in
// internal/agent/blocks_test.go), at least one of each block type and
// shape, and some malformed ones. The gallery renders them; so do the
// tests. say is what Alexa said above the block, for the gallery.

export type Fixture = { name: string; block: Block; say?: string }

// Appendix A.6, verbatim.
export const specFixtures: Fixture[] = [
  {
    name: 'cards: product with a pick',
    block: {
      type: 'cards',
      kind: 'product',
      items: [
        {
          title: 'Sony WH-1000XM6',
          price: { amount: 279, currency: 'USD', was: 329 },
          rating: { value: 4.7, count: 3120 },
          badges: ['Best noise cancelling'],
          facts: [{ label: 'Battery', value: '30 h' }],
          note: 'Quietest on planes',
          pick: true,
          action: { do: 'reply', label: 'Choose this', text: "Let's go with the Sony WH-1000XM6 ($279)." },
        },
      ],
    },
  },
  {
    name: 'compare: top 3',
    block: {
      type: 'compare',
      title: 'Top 3',
      items: [{ name: 'Sony XM6', pick: true }, { name: 'Bose QC Ultra' }, { name: 'AirPods Max' }],
      rows: [
        { label: 'Price', values: ['$279', '$299', '$449'], best: 0 },
        { label: 'Battery', values: ['30 h', '24 h', '20 h'], best: 0 },
      ],
      verdict: 'Sony wins on price and battery.',
    },
  },
  {
    name: 'stats: lowest fare',
    block: { type: 'stats', items: [{ label: 'Lowest fare', value: '$412', delta: '-$38', trend: 'down', good: 'down' }] },
  },
  {
    name: 'receipt: draft',
    block: {
      type: 'receipt',
      status: 'draft',
      merchant: 'Best Buy',
      lines: [
        { label: 'Sony WH-1000XM6', value: '$279.00' },
        { label: 'Tax', value: '$24.76' },
      ],
      total: { amount: 303.76, currency: 'USD' },
    },
  },
  {
    name: 'timeline: a day in New York',
    block: {
      type: 'timeline',
      days: [
        {
          date: '2026-11-14',
          items: [
            { time: '08:10', title: 'UA 1543 SFO to JFK', kind: 'flight' },
            { time: '19:30', title: 'Dinner at Via Carota', kind: 'meal', place: '51 Grove St' },
          ],
        },
      ],
    },
  },
]

// One or more of every other type and shape.
export const moreFixtures: Fixture[] = [
  {
    name: 'cards: three products',
    block: {
      type: 'cards',
      kind: 'product',
      title: 'Noise-cancelling headphones',
      items: [
        { title: 'Sony WH-1000XM6', subtitle: 'Sony', image: photos.headphones, price: { amount: 279, currency: 'USD', was: 329 }, rating: { value: 4.7, count: 3120 }, note: 'Quietest on planes; the case is bulkier.', pick: true, url: 'https://www.sony.com/', action: { do: 'reply', label: 'Choose this', text: 'Let’s go with the Sony WH-1000XM6 ($279).' } },
        { title: 'Bose QuietComfort Ultra', subtitle: 'Bose', image: photos.headphonesSilver, price: { amount: 299, currency: 'USD' }, rating: { value: 4.5, count: 2210 }, badges: ['Most comfortable'], action: { do: 'reply', label: 'Choose this', text: 'Let’s go with the Bose QuietComfort Ultra ($299).' } },
        { title: 'Apple AirPods Max', subtitle: 'Apple', image: photos.headphonesBlue, price: { amount: 449, currency: 'USD' }, rating: { value: 4.4, count: 5400 }, facts: [{ label: 'Battery', value: '20 h' }, { label: 'Weight', value: '385 g' }], action: { do: 'reply', label: 'Choose this', text: 'Let’s go with the AirPods Max ($449).' } },
      ],
    },
  },
  {
    name: 'cards: places',
    block: {
      type: 'cards',
      kind: 'place',
      items: [
        { title: 'Via Carota', subtitle: 'Italian · West Village', image: photos.trattoria, address: '51 Grove St, New York', hours: 'Until 23:00', open_now: true, distance: '0.4 mi', rating: { value: 4.6, count: 1800 }, phone: '+1 212 255 1962', lat: 40.7332, lng: -74.0036, pick: true, action: { do: 'reply', label: 'Book 8pm', text: 'Book Via Carota for 2 at 8pm.' } },
        { title: 'L’Artusi', subtitle: 'Italian · West Village', image: photos.bistro, address: '228 W 10th St, New York', hours: 'Opens 17:30', open_now: false, distance: '0.6 mi', rating: { value: 4.5, count: 1300 }, phone: '+1 212 255 5757' },
      ],
    },
  },
  {
    name: 'cards: a stay',
    block: {
      type: 'cards',
      kind: 'stay',
      items: [{ title: 'The Marlton Hotel', subtitle: 'Greenwich Village', image: photos.room, price: { amount: 389, currency: 'USD', per: 'night', was: 449 }, check_in: '2026-11-14', check_out: '2026-11-17', rating: { value: 4.3, count: 940 }, badges: ['Free cancellation'], note: 'Quiet rooms face the courtyard; ask for one.', url: 'https://www.marltonhotel.com/', action: { do: 'reply', label: 'Book it', text: 'Book the Marlton for Nov 14 to 17.' } }],
    },
  },
  {
    name: 'cards: a flight',
    block: {
      type: 'cards',
      kind: 'flight',
      items: [
        {
          title: 'United 1543',
          price: { amount: 412, currency: 'USD', per: 'person' },
          segments: [{ carrier: 'United', number: 'UA 1543', from: 'SFO', to: 'JFK', depart: '2026-11-14T08:10', arrive: '2026-11-14T16:45' }],
          duration: '5h 35m',
          stops: 0,
          cabin: 'Economy',
        },
      ],
    },
  },
  {
    name: 'cards: an event',
    block: { type: 'cards', kind: 'event', items: [{ title: 'Hadestown', subtitle: 'Musical · 2 h 30 min', venue: 'Walter Kerr Theatre, 219 W 48th St', start: '2026-11-15T19:00', end: '2026-11-15T21:30', price: { amount: 129, currency: 'USD', per: 'person' }, badges: ['Orchestra'], action: { do: 'open', label: 'Tickets', url: 'https://www.hadestown.com/' } }] },
  },
  {
    name: 'table: prices by store',
    block: {
      type: 'table',
      title: 'Prices by store',
      columns: ['Store', 'Price', 'Delivery'],
      align: ['left', 'right', 'left'],
      rows: [
        ['Best Buy', '$279.00', 'Tomorrow'],
        ['Amazon', '$284.99', 'Saturday'],
        ['B&H', '$279.00', 'Monday'],
      ],
    },
  },
  {
    name: 'stats: a month of spending',
    block: {
      type: 'stats',
      items: [
        { label: 'Spent', value: '$2,340', delta: '+$180', trend: 'up', good: 'down', note: 'vs October' },
        { label: 'Groceries', value: '$612', delta: '-$40', trend: 'down', good: 'down' },
        { label: 'Saved', value: '$900', trend: 'flat' },
      ],
    },
  },
  {
    name: 'chart: fare over time',
    block: {
      type: 'chart',
      title: 'SFO to JFK, lowest fare',
      style: 'line',
      x: 'time',
      currency: 'USD',
      series: [
        {
          name: 'United',
          points: [
            { x: '2026-10-01', y: 489 },
            { x: '2026-10-08', y: 462 },
            { x: '2026-10-15', y: 450 },
            { x: '2026-10-22', y: 431 },
            { x: '2026-10-29', y: 412 },
          ],
        },
        {
          name: 'Delta',
          points: [
            { x: '2026-10-01', y: 512 },
            { x: '2026-10-08', y: 498 },
            { x: '2026-10-15', y: 470 },
            { x: '2026-10-22', y: 466 },
            { x: '2026-10-29', y: 455 },
          ],
        },
      ],
      marks: [{ x: '2026-10-29', label: 'Today' }],
    },
  },
  {
    name: 'chart: bars by category',
    block: {
      type: 'chart',
      style: 'bar',
      x: 'category',
      unit: 'h',
      series: [
        {
          name: 'Battery',
          points: [
            { x: 'Sony', y: 30 },
            { x: 'Bose', y: 24 },
            { x: 'Apple', y: 20 },
          ],
        },
      ],
    },
  },
  {
    name: 'checklist: partly done',
    block: {
      type: 'checklist',
      title: 'Trip bookings',
      items: [
        { text: 'Flight SFO to JFK', status: 'done' },
        { text: 'Hotel, 3 nights', status: 'done' },
        { text: 'Dinner on Saturday', status: 'failed', note: 'Fully booked; waitlisted' },
        { text: 'Theatre tickets', status: 'todo' },
        { text: 'Car to the airport', status: 'skipped' },
      ],
    },
  },
  {
    name: 'receipt: confirmed',
    block: {
      type: 'receipt',
      status: 'confirmed',
      merchant: 'United Airlines',
      lines: [
        { label: 'UA 1543 SFO to JFK', value: '$389.00' },
        { label: 'Taxes and fees', value: '$23.00' },
      ],
      total: { amount: 412, currency: 'USD' },
      ref: 'K7Q2ZP',
    },
  },
  {
    name: 'links',
    block: {
      type: 'links',
      items: [
        { title: 'Sony WH-1000XM6 review', url: 'https://www.theverge.com/', note: 'The best all-rounder' },
        { title: 'Rtings comparison', url: 'https://www.rtings.com/' },
      ],
    },
  },
  {
    name: 'images',
    block: {
      type: 'images',
      items: [
        { src: photos.room, caption: 'A king room at the Marlton' },
        { src: photos.lobby, caption: 'The lobby bar', url: 'https://www.marltonhotel.com/' },
      ],
    },
  },
  { name: 'files', block: { type: 'files', items: ['reports/trip-plan.pdf', 'downloads/boarding-pass.pkpass'] } },
  { name: 'callout: tip', block: { type: 'callout', tone: 'tip', text: 'Fares on this route usually drop on Tuesdays. I’ll keep watching and tell you if it goes under $400.' } },
  { name: 'callout: warning', block: { type: 'callout', tone: 'warning', text: 'The hotel’s cancellation window closes on November 10.' } },
  { name: 'markdown (reports only)', block: { type: 'markdown', text: '## Why the Sony\n\nIt’s the **quietest** on planes and lasts the longest. The Bose is more comfortable for long wear.' } },
]


// Bigger and odder shapes: a carousel, a flight with a connection, a
// four-way comparison with prices, a long table, a trip over days, every
// receipt status, a gallery.
export const shapeFixtures: Fixture[] = [
  {
    name: 'cards: six products (carousel)',
    say: 'Six worth a look under $350. The Sony is my pick for flights.',
    block: {
      type: 'cards',
      kind: 'product',
      title: 'Under $350',
      items: [
        { title: 'Sony WH-1000XM6', subtitle: 'Sony', image: photos.headphones, price: { amount: 279, currency: 'USD', was: 329 }, rating: { value: 4.7, count: 3120 }, pick: true, action: { do: 'reply', label: 'Choose this', text: 'Let’s go with the Sony WH-1000XM6 ($279).' } },
        { title: 'Bose QuietComfort Ultra', subtitle: 'Bose', image: photos.headphonesSilver, price: { amount: 299, currency: 'USD' }, rating: { value: 4.5, count: 2210 }, badges: ['Most comfortable'] },
        { title: 'Sennheiser Momentum 4', subtitle: 'Sennheiser', image: photos.headphonesBlue, price: { amount: 249, currency: 'USD', was: 379 }, rating: { value: 4.4, count: 1640 }, facts: [{ label: 'Battery', value: '60 h' }] },
        { title: 'Sony WH-1000XM5', subtitle: 'Sony', image: photos.headphones, price: { amount: 228, currency: 'USD', was: 399 }, rating: { value: 4.6, count: 9800 } },
        { title: 'Bowers & Wilkins Px7 S2e', subtitle: 'Bowers & Wilkins', image: photos.headphonesSilver, price: { amount: 339, currency: 'USD' }, rating: { value: 4.3, count: 610 } },
        { title: 'Anker Soundcore Space One', subtitle: 'Anker', image: photos.headphonesBlue, price: { amount: 79, currency: 'USD', was: 99 }, rating: { value: 4.2, count: 4400 }, badges: ['Budget pick'] },
      ],
    },
  },
  {
    name: 'cards: a flight with a connection',
    say: 'The cheapest way there connects in Chicago and lands the next morning.',
    block: {
      type: 'cards',
      kind: 'flight',
      items: [
        {
          title: 'United, via Chicago',
          price: { amount: 368, currency: 'USD', per: 'person' },
          segments: [
            { carrier: 'United', number: 'UA 512', from: 'SFO', to: 'ORD', depart: '2026-11-13T22:05', arrive: '2026-11-14T04:12' },
            { carrier: 'United', number: 'UA 1180', from: 'ORD', to: 'JFK', depart: '2026-11-14T06:00', arrive: '2026-11-14T09:05' },
          ],
          duration: '8h 00m',
          stops: 1,
          cabin: 'Economy',
          badges: ['1 checked bag'],
        },
        {
          title: 'JetBlue 916',
          price: { amount: 431, currency: 'USD', per: 'person' },
          segments: [{ carrier: 'JetBlue', number: 'B6 916', from: 'SFO', to: 'JFK', depart: '2026-11-14T07:00-08:00', arrive: '2026-11-14T15:31-05:00' }],
          duration: '5h 31m',
          stops: 0,
          cabin: 'Mint',
          pick: true,
          action: { do: 'reply', label: 'Book this', text: 'Book JetBlue 916 on Nov 14.' },
        },
      ],
    },
  },
  {
    name: 'compare: four with prices',
    say: 'Here’s how they stack up.',
    block: {
      type: 'compare',
      title: 'Noise-cancelling headphones',
      items: [
        { name: 'Sony XM6', price: { amount: 279, currency: 'USD' }, pick: true },
        { name: 'Bose QC Ultra', price: { amount: 299, currency: 'USD' } },
        { name: 'Sennheiser M4', price: { amount: 249, currency: 'USD' } },
        { name: 'AirPods Max', price: { amount: 449, currency: 'USD' } },
      ],
      rows: [
        { label: 'Noise cancelling', values: ['Excellent', 'Excellent', 'Very good', 'Very good'], best: 0 },
        { label: 'Battery', values: ['30 h', '24 h', '60 h', '20 h'], best: 2 },
        { label: 'Comfort', values: ['Good', 'Excellent', 'Good', 'Fair'], best: 1 },
        { label: 'Weight', values: ['250 g', '254 g', '293 g', '385 g'], best: 0 },
      ],
      verdict: 'Sony for travel; the Sennheiser if battery matters most.',
    },
  },
  {
    name: 'table: 23 rows (a report)',
    block: {
      type: 'table',
      title: 'Fares by day',
      columns: ['Date', 'Airline', 'Stops', 'Fare'],
      rows: Array.from({ length: 23 }, (_, i) => [`Nov ${i + 1}`, ['United', 'Delta', 'JetBlue', 'American'][i % 4], i % 3 ? 'Nonstop' : '1 stop', `$${(389 + ((i * 37) % 120)).toFixed(2)}`]),
    },
  },
  {
    name: 'table: six columns',
    say: 'All six stores, side by side.',
    block: {
      type: 'table',
      title: 'Where to buy',
      columns: ['Store', 'Price', 'Shipping', 'Arrives', 'Returns', 'Rating'],
      rows: [
        ['Best Buy', '$279.00', 'Free', 'Tomorrow', '15 days', '4.6'],
        ['Amazon', '$284.99', 'Free', 'Sat, Nov 1', '30 days', '4.7'],
        ['B&H Photo', '$279.00', '$4.99', 'Mon, Nov 3', '30 days', '4.8'],
        ['Target', '$299.99', 'Free', 'Tue, Nov 4', '90 days', '4.5'],
      ],
    },
  },
  {
    name: 'timeline: a weekend',
    say: 'Here’s the weekend. Everything’s booked except dinner on Saturday.',
    block: {
      type: 'timeline',
      title: 'New York',
      days: [
        {
          date: '2026-11-14',
          items: [
            { time: '07:00', end: '15:31', title: 'JetBlue 916 SFO to JFK', kind: 'flight', note: 'Seat 3A · conf. QX7P2L' },
            { time: '17:00', title: 'Check in at the Marlton', kind: 'stay', place: '5 W 8th St' },
            { time: '19:30', title: 'Dinner at Via Carota', kind: 'meal', place: '51 Grove St', note: 'Walk-in only; I’ll put your name down at 7' },
          ],
        },
        {
          date: '2026-11-15',
          items: [
            { time: '10:00', title: 'The Met', kind: 'event', place: '1000 5th Ave' },
            { time: '13:00', title: 'Lunch near the park', kind: 'meal' },
            { time: '19:00', end: '21:30', title: 'Hadestown', kind: 'event', place: 'Walter Kerr Theatre' },
          ],
        },
        { date: '2026-11-16', items: [{ time: '11:00', title: 'Car to JFK', kind: 'travel' }, { title: 'Send Mum the photos', kind: 'task' }] },
      ],
    },
  },
  {
    name: 'stats: four tiles',
    say: 'October, in four numbers.',
    block: {
      type: 'stats',
      title: 'October',
      items: [
        { label: 'Spent', value: '$2,340', delta: '+$180', trend: 'up', good: 'down', note: 'vs September' },
        { label: 'Saved', value: '$900', delta: '+$120', trend: 'up', good: 'up' },
        { label: 'Subscriptions', value: '11', delta: '−2', trend: 'down', good: 'down' },
        { label: 'Biggest day', value: 'Oct 12' },
      ],
    },
  },
  {
    name: 'chart: three series, with a mark',
    say: 'Delta dipped below United this week.',
    block: {
      type: 'chart',
      title: 'Lowest fare, last 6 weeks',
      style: 'line',
      x: 'time',
      currency: 'USD',
      series: [
        { name: 'United', points: ['09-24', '10-01', '10-08', '10-15', '10-22', '10-29'].map((d, i) => ({ x: `2026-${d}`, y: [489, 470, 462, 450, 431, 412][i] })) },
        { name: 'Delta', points: ['09-24', '10-01', '10-08', '10-15', '10-22', '10-29'].map((d, i) => ({ x: `2026-${d}`, y: [512, 498, 470, 466, 420, 405][i] })) },
        { name: 'JetBlue', points: ['09-24', '10-01', '10-08', '10-15', '10-22', '10-29'].map((d, i) => ({ x: `2026-${d}`, y: [455, 455, 449, 452, 444, 431][i] })) },
      ],
      marks: [{ x: '2026-10-15', label: 'Sale' }],
    },
  },
  {
    name: 'chart: bars, two series',
    block: {
      type: 'chart',
      title: 'Battery and charge time',
      style: 'bar',
      x: 'category',
      unit: 'h',
      series: [
        { name: 'Battery', points: [{ x: 'Sony', y: 30 }, { x: 'Bose', y: 24 }, { x: 'Sennheiser', y: 60 }, { x: 'Apple', y: 20 }] },
        { name: 'With ANC off', points: [{ x: 'Sony', y: 40 }, { x: 'Bose', y: 30 }, { x: 'Sennheiser', y: 75 }, { x: 'Apple', y: 22 }] },
      ],
    },
  },
  {
    name: 'receipt: pending',
    block: { type: 'receipt', status: 'pending', title: 'Table for 2', merchant: 'Via Carota', lines: [{ label: 'Saturday, Nov 14', value: '19:30' }, { label: 'Deposit', value: '$40.00' }], total: { amount: 40, currency: 'USD' } },
  },
  {
    name: 'receipt: failed',
    say: 'The payment didn’t go through. Nothing was charged.',
    block: { type: 'receipt', status: 'failed', merchant: 'Walter Kerr Theatre', lines: [{ label: 'Hadestown × 2', value: '$258.00' }, { label: 'Fees', value: '$21.40' }, { label: 'Pay with', value: 'Visa ···· 4242' }], total: { amount: 279.4, currency: 'USD' } },
  },
  {
    name: 'receipt: cancelled, in euros',
    block: { type: 'receipt', status: 'cancelled', merchant: 'Hotel Memmo Alfama', lines: [{ label: '2 nights', value: '€418.00' }], total: { amount: 418, currency: 'EUR' }, ref: 'MEM-22931' },
  },
  {
    name: 'images: six (carousel)',
    block: {
      type: 'images',
      title: 'The Marlton',
      items: [
        { src: photos.room, caption: 'A king room' },
        { src: photos.lobby, caption: 'The lobby bar' },
        { src: photos.trattoria, caption: 'Margaux, downstairs' },
        { src: photos.skyline, caption: 'The view from the roof' },
        { src: photos.bistro, caption: 'Breakfast' },
        { src: photos.theatre, caption: 'Around the corner' },
      ],
    },
  },
  {
    name: 'images: one, wide',
    block: { type: 'images', items: [{ src: photos.skyline, caption: 'Lower Manhattan from the Marlton’s roof' }] },
  },
  {
    name: 'images: files that are gone',
    block: { type: 'images', items: [{ src: 'reports/marlton-room.jpg', caption: 'A king room at the Marlton' }, { src: 'reports/marlton-lobby.jpg', caption: 'The lobby bar' }] },
  },
  {
    name: 'cards: an item, no picture',
    block: {
      type: 'cards',
      items: [{ title: 'Renew your passport', subtitle: 'gov.uk · about 3 weeks', facts: [{ label: 'Fee', value: '£88.50' }, { label: 'Expires', value: 'Jan 4, 2027' }], note: 'Do it before December; the queue doubles in spring.', action: { do: 'open', label: 'Start', url: 'https://www.gov.uk/renew-adult-passport' } }],
    },
  },
  { name: 'callout: info', block: { type: 'callout', tone: 'info', text: 'Prices include taxes but not resort fees, which are $35 a night here.' } },
  { name: 'callout: success, with a title', block: { type: 'callout', tone: 'success', title: 'All booked', text: 'Flights, hotel and theatre are confirmed. I put everything in your calendar.' } },
]

// Blocks that don't check out: each shows its summary in its place (or a
// plain line), never a crash.
export const malformedFixtures: Fixture[] = [
  { name: 'malformed: cards without items', block: { type: 'cards', title: 'Hotels in Alfama', items: undefined } as unknown as Block },
  { name: 'malformed: a card without a title', block: { type: 'cards', kind: 'product', items: [{ price: { amount: 279, currency: 'USD' } }] } as unknown as Block },
  { name: 'malformed: chart with a text value', block: { type: 'chart', title: 'Price history', style: 'line', x: 'time', series: [{ name: 'Amazon', points: [{ x: '2026-10-01', y: 'cheap' }] }] } as unknown as Block },
  { name: 'malformed: receipt without a total', block: { type: 'receipt', status: 'confirmed', merchant: 'Best Buy', lines: [] } as unknown as Block },
  { name: 'malformed: compare without rows', block: { type: 'compare', items: [{ name: 'Sony' }, { name: 'Bose' }] } as unknown as Block },
]

export const fixtures: Fixture[] = [...specFixtures, ...moreFixtures, ...shapeFixtures]
