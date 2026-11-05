// Stand-in photos for the gallery's fixtures: tiny SVG scenes as data URLs,
// so the gallery shows pictures with no network and no workspace. They use
// named colours only (tokens.test.ts keeps colour literals out of code).
// The server never sends data URLs; real blocks carry /api/img tokens or
// workspace paths.

const svg = (w: number, h: number, body: string) => `data:image/svg+xml;utf8,${encodeURIComponent(`<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 ${w} ${h}' width='${w}' height='${h}'>${body}</svg>`)}`

export const photos = {
  // A product shot on white, as shops photograph them.
  headphones: svg(
    400,
    400,
    `<rect width='400' height='400' fill='white'/><path d='M110 230 a90 110 0 0 1 180 0' fill='none' stroke='black' stroke-width='18' stroke-linecap='round'/><rect x='88' y='215' width='54' height='92' rx='24' fill='black'/><rect x='258' y='215' width='54' height='92' rx='24' fill='black'/><rect x='100' y='228' width='30' height='66' rx='14' fill='dimgray'/><rect x='270' y='228' width='30' height='66' rx='14' fill='dimgray'/>`,
  ),
  headphonesSilver: svg(
    400,
    400,
    `<rect width='400' height='400' fill='white'/><path d='M110 230 a90 110 0 0 1 180 0' fill='none' stroke='silver' stroke-width='18' stroke-linecap='round'/><rect x='88' y='215' width='54' height='92' rx='24' fill='darkgray'/><rect x='258' y='215' width='54' height='92' rx='24' fill='darkgray'/><rect x='100' y='228' width='30' height='66' rx='14' fill='gainsboro'/><rect x='270' y='228' width='30' height='66' rx='14' fill='gainsboro'/>`,
  ),
  headphonesBlue: svg(
    400,
    400,
    `<rect width='400' height='400' fill='white'/><path d='M120 220 a80 100 0 0 1 160 0' fill='none' stroke='midnightblue' stroke-width='14' stroke-linecap='round'/><ellipse cx='118' cy='262' rx='34' ry='48' fill='midnightblue'/><ellipse cx='282' cy='262' rx='34' ry='48' fill='midnightblue'/><ellipse cx='118' cy='262' rx='18' ry='30' fill='steelblue'/><ellipse cx='282' cy='262' rx='18' ry='30' fill='steelblue'/>`,
  ),
  room: svg(
    800,
    600,
    `<rect width='800' height='600' fill='wheat'/><rect y='420' width='800' height='180' fill='peru'/><rect x='520' y='70' width='200' height='240' fill='lightsteelblue'/><rect x='520' y='70' width='200' height='240' fill='none' stroke='white' stroke-width='10'/><rect x='170' y='230' width='360' height='120' rx='10' fill='sienna'/><rect x='150' y='330' width='400' height='120' rx='14' fill='linen'/><rect x='190' y='300' width='120' height='50' rx='18' fill='white'/><rect x='390' y='300' width='120' height='50' rx='18' fill='white'/><rect x='150' y='400' width='400' height='50' fill='tan'/><circle cx='90' cy='300' r='34' fill='gold' opacity='.8'/><rect x='84' y='330' width='12' height='110' fill='dimgray'/>`,
  ),
  lobby: svg(
    800,
    600,
    `<rect width='800' height='600' fill='darkslategray'/><rect y='440' width='800' height='160' fill='saddlebrown'/><rect x='0' y='400' width='800' height='50' fill='sienna'/><circle cx='160' cy='120' r='26' fill='goldenrod'/><circle cx='400' cy='100' r='26' fill='goldenrod'/><circle cx='640' cy='120' r='26' fill='goldenrod'/><rect x='100' y='180' width='600' height='170' fill='teal' opacity='.5'/><rect x='120' y='200' width='18' height='140' fill='burlywood'/><rect x='170' y='230' width='18' height='110' fill='olive'/><rect x='220' y='210' width='18' height='130' fill='maroon'/><rect x='270' y='240' width='18' height='100' fill='burlywood'/>`,
  ),
  trattoria: svg(
    800,
    600,
    `<rect width='800' height='600' fill='maroon'/><rect y='360' width='800' height='240' fill='burlywood'/><ellipse cx='260' cy='430' rx='120' ry='40' fill='oldlace'/><ellipse cx='560' cy='450' rx='120' ry='40' fill='oldlace'/><ellipse cx='260' cy='425' rx='70' ry='20' fill='tomato'/><ellipse cx='560' cy='445' rx='70' ry='20' fill='darkolivegreen'/><circle cx='120' cy='120' r='40' fill='orange' opacity='.7'/><circle cx='680' cy='100' r='40' fill='orange' opacity='.7'/>`,
  ),
  bistro: svg(
    800,
    600,
    `<rect width='800' height='600' fill='darkolivegreen'/><rect y='380' width='800' height='220' fill='tan'/><rect x='60' y='80' width='300' height='220' fill='khaki' opacity='.6'/><rect x='440' y='80' width='300' height='220' fill='khaki' opacity='.6'/><ellipse cx='400' cy='450' rx='160' ry='46' fill='white'/><ellipse cx='400' cy='445' rx='90' ry='22' fill='goldenrod'/>`,
  ),
  skyline: svg(
    800,
    600,
    `<rect width='800' height='600' fill='lightsteelblue'/><circle cx='640' cy='130' r='50' fill='lemonchiffon'/><rect x='40' y='260' width='110' height='340' fill='slategray'/><rect x='170' y='180' width='90' height='420' fill='lightslategray'/><rect x='280' y='300' width='130' height='300' fill='slategray'/><rect x='430' y='120' width='80' height='480' fill='dimgray'/><rect x='530' y='240' width='120' height='360' fill='lightslategray'/><rect x='670' y='320' width='110' height='280' fill='slategray'/>`,
  ),
  theatre: svg(
    800,
    600,
    `<rect width='800' height='600' fill='black'/><path d='M0 0 H800 V140 Q600 220 400 140 Q200 220 0 140 Z' fill='darkred'/><rect x='0' y='0' width='120' height='600' fill='darkred'/><rect x='680' y='0' width='120' height='600' fill='darkred'/><ellipse cx='400' cy='520' rx='300' ry='40' fill='goldenrod' opacity='.5'/><circle cx='400' cy='330' r='60' fill='wheat' opacity='.85'/>`,
  ),
}
