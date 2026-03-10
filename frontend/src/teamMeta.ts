// NBA, NCAAB, and NHL team metadata: display names and ESPN CDN logo URLs.
// Keyed by the Polymarket outcome name (nickname) for NBA/NHL,
// and by Polymarket location/display name for NCAAB.

export interface TeamMeta {
  abbr: string
  city: string
  logo: string
}

const espnNBA = (abbr: string) =>
  `https://a.espncdn.com/i/teamlogos/nba/500/${abbr.toLowerCase()}.png`

const espnNCAAB = (id: number) =>
  `https://a.espncdn.com/i/teamlogos/ncaa/500/${id}.png`

const espnNHL = (abbr: string) =>
  `https://a.espncdn.com/i/teamlogos/nhl/500/${abbr.toLowerCase()}.png`

// All 30 NBA teams, keyed by the nickname Polymarket uses in outcomes.
export const NBA_TEAMS: Record<string, TeamMeta> = {
  'Hawks':          { abbr: 'ATL', city: 'Atlanta',       logo: espnNBA('atl') },
  'Celtics':        { abbr: 'BOS', city: 'Boston',        logo: espnNBA('bos') },
  'Nets':           { abbr: 'BKN', city: 'Brooklyn',      logo: espnNBA('bkn') },
  'Hornets':        { abbr: 'CHA', city: 'Charlotte',     logo: espnNBA('cha') },
  'Bulls':          { abbr: 'CHI', city: 'Chicago',       logo: espnNBA('chi') },
  'Cavaliers':      { abbr: 'CLE', city: 'Cleveland',     logo: espnNBA('cle') },
  'Mavericks':      { abbr: 'DAL', city: 'Dallas',        logo: espnNBA('dal') },
  'Nuggets':        { abbr: 'DEN', city: 'Denver',        logo: espnNBA('den') },
  'Pistons':        { abbr: 'DET', city: 'Detroit',       logo: espnNBA('det') },
  'Warriors':       { abbr: 'GSW', city: 'Golden State',  logo: espnNBA('gs') },
  'Rockets':        { abbr: 'HOU', city: 'Houston',       logo: espnNBA('hou') },
  'Pacers':         { abbr: 'IND', city: 'Indiana',       logo: espnNBA('ind') },
  'Clippers':       { abbr: 'LAC', city: 'LA',            logo: espnNBA('lac') },
  'Lakers':         { abbr: 'LAL', city: 'Los Angeles',   logo: espnNBA('lal') },
  'Grizzlies':      { abbr: 'MEM', city: 'Memphis',       logo: espnNBA('mem') },
  'Heat':           { abbr: 'MIA', city: 'Miami',         logo: espnNBA('mia') },
  'Bucks':          { abbr: 'MIL', city: 'Milwaukee',     logo: espnNBA('mil') },
  'Timberwolves':   { abbr: 'MIN', city: 'Minnesota',     logo: espnNBA('min') },
  'Pelicans':       { abbr: 'NOP', city: 'New Orleans',   logo: espnNBA('no') },
  'Knicks':         { abbr: 'NYK', city: 'New York',      logo: espnNBA('ny') },
  'Thunder':        { abbr: 'OKC', city: 'Oklahoma City', logo: espnNBA('okc') },
  'Magic':          { abbr: 'ORL', city: 'Orlando',       logo: espnNBA('orl') },
  '76ers':          { abbr: 'PHI', city: 'Philadelphia',  logo: espnNBA('phi') },
  'Suns':           { abbr: 'PHX', city: 'Phoenix',       logo: espnNBA('phx') },
  'Trail Blazers':  { abbr: 'POR', city: 'Portland',      logo: espnNBA('por') },
  'Kings':          { abbr: 'SAC', city: 'Sacramento',    logo: espnNBA('sac') },
  'Spurs':          { abbr: 'SAS', city: 'San Antonio',   logo: espnNBA('sa') },
  'Raptors':        { abbr: 'TOR', city: 'Toronto',       logo: espnNBA('tor') },
  'Jazz':           { abbr: 'UTA', city: 'Utah',          logo: espnNBA('utah') },
  'Wizards':        { abbr: 'WAS', city: 'Washington',    logo: espnNBA('was') },
}

// Major NCAAB teams (March Madness / power conferences).
// ESPN uses numeric team IDs for NCAAB logos.
export const NCAAB_TEAMS: Record<string, TeamMeta> = {
  'Duke':               { abbr: 'DUKE', city: 'Duke',              logo: espnNCAAB(150) },
  'Duke Blue Devils':   { abbr: 'DUKE', city: 'Duke',              logo: espnNCAAB(150) },
  'North Carolina':     { abbr: 'UNC',  city: 'North Carolina',    logo: espnNCAAB(153) },
  'UNC':                { abbr: 'UNC',  city: 'North Carolina',    logo: espnNCAAB(153) },
  'Kentucky':           { abbr: 'UK',   city: 'Kentucky',          logo: espnNCAAB(96) },
  'Kansas':             { abbr: 'KU',   city: 'Kansas',            logo: espnNCAAB(2305) },
  'Gonzaga':            { abbr: 'GONZ', city: 'Gonzaga',           logo: espnNCAAB(2250) },
  'Villanova':          { abbr: 'NOVA', city: 'Villanova',         logo: espnNCAAB(222) },
  'UCLA':               { abbr: 'UCLA', city: 'UCLA',              logo: espnNCAAB(26) },
  'Michigan':           { abbr: 'MICH', city: 'Michigan',          logo: espnNCAAB(130) },
  'Michigan St':        { abbr: 'MSU',  city: 'Michigan State',    logo: espnNCAAB(127) },
  'Michigan State':     { abbr: 'MSU',  city: 'Michigan State',    logo: espnNCAAB(127) },
  'Ohio State':         { abbr: 'OSU',  city: 'Ohio State',        logo: espnNCAAB(194) },
  'Louisville':         { abbr: 'LOU',  city: 'Louisville',        logo: espnNCAAB(97) },
  'Syracuse':           { abbr: 'SYR',  city: 'Syracuse',          logo: espnNCAAB(183) },
  'UConn':              { abbr: 'UCONN', city: 'UConn',            logo: espnNCAAB(41) },
  'Connecticut':        { abbr: 'UCONN', city: 'UConn',            logo: espnNCAAB(41) },
  'Florida':            { abbr: 'FLA',  city: 'Florida',           logo: espnNCAAB(57) },
  'Auburn':             { abbr: 'AUB',  city: 'Auburn',            logo: espnNCAAB(2) },
  'Alabama':            { abbr: 'BAMA', city: 'Alabama',           logo: espnNCAAB(333) },
  'Tennessee':          { abbr: 'TENN', city: 'Tennessee',         logo: espnNCAAB(2633) },
  'Purdue':             { abbr: 'PUR',  city: 'Purdue',            logo: espnNCAAB(2509) },
  'Houston':            { abbr: 'HOU',  city: 'Houston',           logo: espnNCAAB(248) },
  'Baylor':             { abbr: 'BAY',  city: 'Baylor',            logo: espnNCAAB(239) },
  'Arizona':            { abbr: 'ARIZ', city: 'Arizona',           logo: espnNCAAB(12) },
  'Texas':              { abbr: 'TEX',  city: 'Texas',             logo: espnNCAAB(251) },
  'Virginia':           { abbr: 'UVA',  city: 'Virginia',          logo: espnNCAAB(258) },
  'Marquette':          { abbr: 'MARQ', city: 'Marquette',         logo: espnNCAAB(269) },
  'Creighton':          { abbr: 'CREI', city: 'Creighton',         logo: espnNCAAB(156) },
  'Iowa State':         { abbr: 'ISU',  city: 'Iowa State',        logo: espnNCAAB(66) },
  'Iowa':               { abbr: 'IOWA', city: 'Iowa',              logo: espnNCAAB(2294) },
  'Wisconsin':          { abbr: 'WIS',  city: 'Wisconsin',         logo: espnNCAAB(275) },
  'Illinois':           { abbr: 'ILL',  city: 'Illinois',          logo: espnNCAAB(356) },
  'Indiana':            { abbr: 'IND',  city: 'Indiana',           logo: espnNCAAB(84) },
  'Oregon':             { abbr: 'ORE',  city: 'Oregon',            logo: espnNCAAB(2483) },
  'Arkansas':           { abbr: 'ARK',  city: 'Arkansas',          logo: espnNCAAB(8) },
  'LSU':                { abbr: 'LSU',  city: 'LSU',               logo: espnNCAAB(99) },
  'Texas Tech':         { abbr: 'TTU',  city: 'Texas Tech',        logo: espnNCAAB(2641) },
  'Memphis':            { abbr: 'MEM',  city: 'Memphis',           logo: espnNCAAB(235) },
  'San Diego State':    { abbr: 'SDSU', city: 'San Diego State',   logo: espnNCAAB(21) },
  'Xavier':             { abbr: 'XAV',  city: 'Xavier',            logo: espnNCAAB(2752) },
  'St. Johns':          { abbr: 'SJU',  city: "St. John's",        logo: espnNCAAB(2599) },
  "St. John's":        { abbr: 'SJU',  city: "St. John's",        logo: espnNCAAB(2599) },
  'Colorado':           { abbr: 'COLO', city: 'Colorado',          logo: espnNCAAB(38) },
  'BYU':                { abbr: 'BYU',  city: 'BYU',               logo: espnNCAAB(252) },
  'Pittsburgh':         { abbr: 'PITT', city: 'Pittsburgh',        logo: espnNCAAB(221) },
  'Clemson':            { abbr: 'CLEM', city: 'Clemson',           logo: espnNCAAB(228) },
  'Miami':              { abbr: 'MIA',  city: 'Miami',             logo: espnNCAAB(2390) },
  'Georgetown':         { abbr: 'GTWN', city: 'Georgetown',        logo: espnNCAAB(46) },
  'USC':                { abbr: 'USC',  city: 'USC',               logo: espnNCAAB(30) },
  'Stanford':           { abbr: 'STAN', city: 'Stanford',          logo: espnNCAAB(24) },
  'Oklahoma':           { abbr: 'OU',   city: 'Oklahoma',          logo: espnNCAAB(201) },
  'Maryland':           { abbr: 'MD',   city: 'Maryland',          logo: espnNCAAB(120) },
  'Georgia':            { abbr: 'UGA',  city: 'Georgia',           logo: espnNCAAB(61) },
  'Mississippi State':  { abbr: 'MSST', city: 'Mississippi State', logo: espnNCAAB(344) },
  'Ole Miss':           { abbr: 'MISS', city: 'Ole Miss',          logo: espnNCAAB(145) },
  'Nebraska':           { abbr: 'NEB',  city: 'Nebraska',          logo: espnNCAAB(158) },
  'Penn State':         { abbr: 'PSU',  city: 'Penn State',        logo: espnNCAAB(213) },
  'Northwestern':       { abbr: 'NW',   city: 'Northwestern',      logo: espnNCAAB(77) },
  'Minnesota':          { abbr: 'MINN', city: 'Minnesota',         logo: espnNCAAB(135) },
  'Rutgers':            { abbr: 'RUT',  city: 'Rutgers',           logo: espnNCAAB(164) },
  'NC State':           { abbr: 'NCST', city: 'NC State',          logo: espnNCAAB(152) },
  'Wake Forest':        { abbr: 'WAKE', city: 'Wake Forest',       logo: espnNCAAB(154) },
  'Virginia Tech':      { abbr: 'VT',   city: 'Virginia Tech',     logo: espnNCAAB(259) },
  'Florida State':      { abbr: 'FSU',  city: 'Florida State',     logo: espnNCAAB(52) },
  'Georgia Tech':       { abbr: 'GT',   city: 'Georgia Tech',      logo: espnNCAAB(59) },
  'Notre Dame':         { abbr: 'ND',   city: 'Notre Dame',        logo: espnNCAAB(87) },
  'Providence':         { abbr: 'PROV', city: 'Providence',        logo: espnNCAAB(2507) },
  'Seton Hall':         { abbr: 'SH',   city: 'Seton Hall',        logo: espnNCAAB(2550) },
  'Butler':             { abbr: 'BUT',  city: 'Butler',            logo: espnNCAAB(2086) },
  'DePaul':             { abbr: 'DEP',  city: 'DePaul',            logo: espnNCAAB(305) },
  'Texas A&M':          { abbr: 'TAMU', city: 'Texas A&M',         logo: espnNCAAB(245) },
  'South Carolina':     { abbr: 'SC',   city: 'South Carolina',    logo: espnNCAAB(2579) },
  'Missouri':           { abbr: 'MIZ',  city: 'Missouri',          logo: espnNCAAB(142) },
  'Vanderbilt':         { abbr: 'VAN',  city: 'Vanderbilt',        logo: espnNCAAB(238) },
  'West Virginia':      { abbr: 'WVU',  city: 'West Virginia',     logo: espnNCAAB(277) },
  'TCU':                { abbr: 'TCU',  city: 'TCU',               logo: espnNCAAB(2628) },
  'Kansas State':       { abbr: 'KSU',  city: 'Kansas State',      logo: espnNCAAB(2306) },
  'Oklahoma State':     { abbr: 'OKST', city: 'Oklahoma State',    logo: espnNCAAB(197) },
  'Cincinnati':         { abbr: 'CIN',  city: 'Cincinnati',        logo: espnNCAAB(2132) },
  'UCF':                { abbr: 'UCF',  city: 'UCF',               logo: espnNCAAB(2116) },
  'Michigan St.':       { abbr: 'MSU',  city: 'Michigan State',    logo: espnNCAAB(127) },
  'Dayton':             { abbr: 'DAY',  city: 'Dayton',            logo: espnNCAAB(2168) },
  'VCU':                { abbr: 'VCU',  city: 'VCU',               logo: espnNCAAB(2670) },
  'Saint Marys':        { abbr: 'SMC',  city: "Saint Mary's",      logo: espnNCAAB(2608) },
  "Saint Mary's":       { abbr: 'SMC',  city: "Saint Mary's",      logo: espnNCAAB(2608) },
  'Nevada':             { abbr: 'NEV',  city: 'Nevada',            logo: espnNCAAB(2440) },
  'Colorado State':     { abbr: 'CSU',  city: 'Colorado State',    logo: espnNCAAB(36) },
  'New Mexico':         { abbr: 'UNM',  city: 'New Mexico',        logo: espnNCAAB(167) },
  'Wichita State':      { abbr: 'WICH', city: 'Wichita State',     logo: espnNCAAB(2724) },
  'Drake':              { abbr: 'DRAK', city: 'Drake',             logo: espnNCAAB(2181) },
  'Loyola Chicago':     { abbr: 'LUC',  city: 'Loyola Chicago',    logo: espnNCAAB(2350) },
}

// All 32 NHL teams, keyed by the nickname Polymarket uses in outcomes.
export const NHL_TEAMS: Record<string, TeamMeta> = {
  'Ducks':          { abbr: 'ANA', city: 'Anaheim',       logo: espnNHL('ana') },
  'Bruins':         { abbr: 'BOS', city: 'Boston',        logo: espnNHL('bos') },
  'Sabres':         { abbr: 'BUF', city: 'Buffalo',       logo: espnNHL('buf') },
  'Flames':         { abbr: 'CGY', city: 'Calgary',       logo: espnNHL('cgy') },
  'Hurricanes':     { abbr: 'CAR', city: 'Carolina',      logo: espnNHL('car') },
  'Blackhawks':     { abbr: 'CHI', city: 'Chicago',       logo: espnNHL('chi') },
  'Avalanche':      { abbr: 'COL', city: 'Colorado',      logo: espnNHL('col') },
  'Blue Jackets':   { abbr: 'CBJ', city: 'Columbus',      logo: espnNHL('cbj') },
  'Stars':          { abbr: 'DAL', city: 'Dallas',        logo: espnNHL('dal') },
  'Red Wings':      { abbr: 'DET', city: 'Detroit',       logo: espnNHL('det') },
  'Oilers':         { abbr: 'EDM', city: 'Edmonton',      logo: espnNHL('edm') },
  'Panthers':       { abbr: 'FLA', city: 'Florida',       logo: espnNHL('fla') },
  'Kings':          { abbr: 'LAK', city: 'Los Angeles',   logo: espnNHL('lak') },
  'Wild':           { abbr: 'MIN', city: 'Minnesota',     logo: espnNHL('min') },
  'Canadiens':      { abbr: 'MTL', city: 'Montréal',      logo: espnNHL('mtl') },
  'Predators':      { abbr: 'NSH', city: 'Nashville',     logo: espnNHL('nsh') },
  'Devils':         { abbr: 'NJD', city: 'New Jersey',    logo: espnNHL('nj') },
  'Islanders':      { abbr: 'NYI', city: 'NY Islanders',  logo: espnNHL('nyi') },
  'Rangers':        { abbr: 'NYR', city: 'NY Rangers',    logo: espnNHL('nyr') },
  'Senators':       { abbr: 'OTT', city: 'Ottawa',        logo: espnNHL('ott') },
  'Flyers':         { abbr: 'PHI', city: 'Philadelphia',  logo: espnNHL('phi') },
  'Penguins':       { abbr: 'PIT', city: 'Pittsburgh',    logo: espnNHL('pit') },
  'Blues':          { abbr: 'STL', city: 'St. Louis',     logo: espnNHL('stl') },
  'Sharks':         { abbr: 'SJS', city: 'San Jose',      logo: espnNHL('sjs') },
  'Kraken':         { abbr: 'SEA', city: 'Seattle',       logo: espnNHL('sea') },
  'Lightning':      { abbr: 'TBL', city: 'Tampa Bay',     logo: espnNHL('tb') },
  'Maple Leafs':    { abbr: 'TOR', city: 'Toronto',       logo: espnNHL('tor') },
  'Utah HC':        { abbr: 'UTA', city: 'Utah',          logo: espnNHL('utah') },
  'Canucks':        { abbr: 'VAN', city: 'Vancouver',     logo: espnNHL('van') },
  'Golden Knights': { abbr: 'VGK', city: 'Vegas',         logo: espnNHL('vgk') },
  'Capitals':       { abbr: 'WSH', city: 'Washington',    logo: espnNHL('wsh') },
  'Jets':           { abbr: 'WPG', city: 'Winnipeg',      logo: espnNHL('wpg') },
}

// Lookup: try NBA, NCAAB, or NHL by competition.
export function getTeamMeta(name: string, competitionId: string): TeamMeta | undefined {
  if (competitionId === 'nba') return NBA_TEAMS[name]
  if (competitionId === 'ncaab') return NCAAB_TEAMS[name]
  if (competitionId === 'nhl') return NHL_TEAMS[name]
  return undefined
}

// Format the display name: "PHI 76ers" / "BOS Bruins" on desktop; original for others.
export function formatTeamName(name: string, competitionId: string): string {
  if (competitionId === 'nba') {
    const meta = NBA_TEAMS[name]
    return meta ? `${meta.abbr} ${name}` : name
  }
  if (competitionId === 'nhl') {
    const meta = NHL_TEAMS[name]
    return meta ? `${meta.abbr} ${name}` : name
  }
  return name
}

// Short form: abbr only for NBA/NCAAB/NHL, original name for others.
export function formatTeamNameShort(name: string, competitionId: string): string {
  const meta = getTeamMeta(name, competitionId)
  return meta ? meta.abbr : name
}
