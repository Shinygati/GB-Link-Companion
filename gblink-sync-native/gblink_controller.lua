-- GB-Link controller and status screens for mGBA.
-- Written by GB-Link Companion. While you play:
--  * the buttons on your real GBA control the game in mGBA (controller mode), or
--  * the GBA shows live info from the Pokemon game running in mGBA (RNG state, the wild
--    Pokemon, the trainer's team, where you are on the map).
-- Supported: Pokemon Ruby, Sapphire, Emerald, FireRed and LeafGreen (English, every revision).
-- mGBA 0.11 / development builds load this automatically. On mGBA 0.10, load it each time with
-- Tools > Scripting... > File > Load script...
local PORT = 47187
local sock, buf, added, wait, tries = nil, "", 0, 0, 0
local mode = 0            -- what the GBA shows: 0 nothing, 1 RNG, 2 wild, 3 trainer, 4 map
local cursor = nil        -- the map cursor's cell {x, y}, or nil (set by GB-Link Companion)
local urgent = false      -- send the screen on the next frame (the cursor moved)
local preview = false     -- show the place under the cursor as a picture (set by GB-Link Companion)
-- Pausing from the RNG screen (GB-Link Companion sends P commands): mGBA stops between frames,
-- steps a frame at a time, then goes on holding the buttons chosen for the first frames.
local paused, steps, holdKeys, holdFrames = false, 0, 0, 0
local quiet = 0           -- frames since the app last sent the GBA's buttons
local HOLD_FRAMES = 6

------------------------------------------------------------------------------- the game
-- Addresses from the pret decompilations' symbol files (pokeruby, pokeemerald, pokefirered).
-- For each header code + revision, the builds it can be (checked against the ROM in detect())
local VERSIONS = {}
local function add(key, t) VERSIONS[key] = VERSIONS[key] or {}; table.insert(VERSIONS[key], t) end
add("AXVE0", { game = "RUBY", rng = 0x03004818, party = 0x03004360, enemy = 0x030045C0, sb1 = 0x02025734, sb2 = 0x02024EA4, mapHeader = 0x0202E828, main = 0x03001770, battleType = 0x020239F8, opponent = 0x0202FF5E, battleMons = 0x02024A80, partyIdx = 0x02024A6A, speciesNames = 0x081F716C, abilityNames = 0x081FA248, natureNames = 0x083C1004, speciesInfo = 0x081FEC18, trainers = 0x081F04FC, classNames = 0x081F0208, mapEntries = 0x083E73C4, mapGroups = 0x08308588, frontPics = 0x081E8354, palettes = 0x081EA5B4, shinyPalettes = 0x081EB374, icons = 0x083BBD20, iconPalIdx = 0x083BC400, iconPals = 0x083BC5B8, rmTiles = 0x083E5DA0, rmTilemap = 0x083E6B04, rmPal = 0x083E5D60, rmCursor = 0x083E5AF0, rmCursorPal = 0x083E5AD0, rmHero = 0x083E5C40, rmHeroPal = 0x083E5C20, rmHeroine = 0x083E5CE0, rmHeroinePal = 0x083E5CC0, navDevTiles = 0x083E05F4, navDevMap = 0x083E0804, navDevPal = 0x083E05D4 })
add("AXVE1", { game = "RUBY", rng = 0x03004818, party = 0x03004360, enemy = 0x030045C0, sb1 = 0x02025734, sb2 = 0x02024EA4, mapHeader = 0x0202E828, main = 0x03001770, battleType = 0x020239F8, opponent = 0x0202FF5E, battleMons = 0x02024A80, partyIdx = 0x02024A6A, speciesNames = 0x081F7184, abilityNames = 0x081FA260, natureNames = 0x083C1020, speciesInfo = 0x081FEC30, trainers = 0x081F0514, classNames = 0x081F0220, mapEntries = 0x083E73E0, mapGroups = 0x083085A0, frontPics = 0x081E836C, palettes = 0x081EA5CC, shinyPalettes = 0x081EB38C, icons = 0x083BBD3C, iconPalIdx = 0x083BC41C, iconPals = 0x083BC5D4, rmTiles = 0x083E5DBC, rmTilemap = 0x083E6B20, rmPal = 0x083E5D7C, rmCursor = 0x083E5B0C, rmCursorPal = 0x083E5AEC, rmHero = 0x083E5C5C, rmHeroPal = 0x083E5C3C, rmHeroine = 0x083E5CFC, rmHeroinePal = 0x083E5CDC, navDevTiles = 0x083E0610, navDevMap = 0x083E0820, navDevPal = 0x083E05F0 })
add("AXVE2", { game = "RUBY", rng = 0x03004818, party = 0x03004360, enemy = 0x030045C0, sb1 = 0x02025734, sb2 = 0x02024EA4, mapHeader = 0x0202E828, main = 0x03001770, battleType = 0x020239F8, opponent = 0x0202FF5E, battleMons = 0x02024A80, partyIdx = 0x02024A6A, speciesNames = 0x081F7184, abilityNames = 0x081FA260, natureNames = 0x083C1020, speciesInfo = 0x081FEC30, trainers = 0x081F0514, classNames = 0x081F0220, mapEntries = 0x083E73E0, mapGroups = 0x083085A0, frontPics = 0x081E836C, palettes = 0x081EA5CC, shinyPalettes = 0x081EB38C, icons = 0x083BBD3C, iconPalIdx = 0x083BC41C, iconPals = 0x083BC5D4, rmTiles = 0x083E5DBC, rmTilemap = 0x083E6B20, rmPal = 0x083E5D7C, rmCursor = 0x083E5B0C, rmCursorPal = 0x083E5AEC, rmHero = 0x083E5C5C, rmHeroPal = 0x083E5C3C, rmHeroine = 0x083E5CFC, rmHeroinePal = 0x083E5CDC, navDevTiles = 0x083E0610, navDevMap = 0x083E0820, navDevPal = 0x083E05F0 })
add("AXPE0", { game = "SAPPHIRE", rng = 0x03004818, party = 0x03004360, enemy = 0x030045C0, sb1 = 0x02025734, sb2 = 0x02024EA4, mapHeader = 0x0202E828, main = 0x03001770, battleType = 0x020239F8, opponent = 0x0202FF5E, battleMons = 0x02024A80, partyIdx = 0x02024A6A, speciesNames = 0x081F70FC, abilityNames = 0x081FA1D8, natureNames = 0x083C105C, speciesInfo = 0x081FEBA8, trainers = 0x081F048C, classNames = 0x081F0198, mapEntries = 0x083E741C, mapGroups = 0x08308518, frontPics = 0x081E82E4, palettes = 0x081EA544, shinyPalettes = 0x081EB304, icons = 0x083BBD78, iconPalIdx = 0x083BC458, iconPals = 0x083BC610, rmTiles = 0x083E5DF8, rmTilemap = 0x083E6B5C, rmPal = 0x083E5DB8, rmCursor = 0x083E5B48, rmCursorPal = 0x083E5B28, rmHero = 0x083E5C98, rmHeroPal = 0x083E5C78, rmHeroine = 0x083E5D38, rmHeroinePal = 0x083E5D18, navDevTiles = 0x083E064C, navDevMap = 0x083E085C, navDevPal = 0x083E062C })
add("AXPE1", { game = "SAPPHIRE", rng = 0x03004818, party = 0x03004360, enemy = 0x030045C0, sb1 = 0x02025734, sb2 = 0x02024EA4, mapHeader = 0x0202E828, main = 0x03001770, battleType = 0x020239F8, opponent = 0x0202FF5E, battleMons = 0x02024A80, partyIdx = 0x02024A6A, speciesNames = 0x081F7114, abilityNames = 0x081FA1F0, natureNames = 0x083C107C, speciesInfo = 0x081FEBC0, trainers = 0x081F04A4, classNames = 0x081F01B0, mapEntries = 0x083E743C, mapGroups = 0x08308530, frontPics = 0x081E82FC, palettes = 0x081EA55C, shinyPalettes = 0x081EB31C, icons = 0x083BBD98, iconPalIdx = 0x083BC478, iconPals = 0x083BC630, rmTiles = 0x083E5E18, rmTilemap = 0x083E6B7C, rmPal = 0x083E5DD8, rmCursor = 0x083E5B68, rmCursorPal = 0x083E5B48, rmHero = 0x083E5CB8, rmHeroPal = 0x083E5C98, rmHeroine = 0x083E5D58, rmHeroinePal = 0x083E5D38, navDevTiles = 0x083E066C, navDevMap = 0x083E087C, navDevPal = 0x083E064C })
add("AXPE2", { game = "SAPPHIRE", rng = 0x03004818, party = 0x03004360, enemy = 0x030045C0, sb1 = 0x02025734, sb2 = 0x02024EA4, mapHeader = 0x0202E828, main = 0x03001770, battleType = 0x020239F8, opponent = 0x0202FF5E, battleMons = 0x02024A80, partyIdx = 0x02024A6A, speciesNames = 0x081F7114, abilityNames = 0x081FA1F0, natureNames = 0x083C107C, speciesInfo = 0x081FEBC0, trainers = 0x081F04A4, classNames = 0x081F01B0, mapEntries = 0x083E743C, mapGroups = 0x08308530, frontPics = 0x081E82FC, palettes = 0x081EA55C, shinyPalettes = 0x081EB31C, icons = 0x083BBD98, iconPalIdx = 0x083BC478, iconPals = 0x083BC630, rmTiles = 0x083E5E18, rmTilemap = 0x083E6B7C, rmPal = 0x083E5DD8, rmCursor = 0x083E5B68, rmCursorPal = 0x083E5B48, rmHero = 0x083E5CB8, rmHeroPal = 0x083E5C98, rmHeroine = 0x083E5D58, rmHeroinePal = 0x083E5D38, navDevTiles = 0x083E066C, navDevMap = 0x083E087C, navDevPal = 0x083E064C })
add("BPEE0", { game = "EMERALD", rng = 0x03005D80, party = 0x020244EC, enemy = 0x02024744, sb1ptr = 0x03005D8C, sb2ptr = 0x03005D90, mapHeader = 0x02037318, main = 0x030022C0, battleType = 0x02022FEC, opponent = 0x02038BCA, battleMons = 0x02024084, battleStructPtr = 0x0202449C, partyIdx = 0x0202406E, speciesNames = 0x083185C8, abilityNames = 0x0831B6DB, natureNames = 0x0861CB50, speciesInfo = 0x083203CC, trainers = 0x08310030, classNames = 0x0830FCD4, mapEntries = 0x085A147C, mapGroups = 0x08486578, frontPics = 0x0830A18C, palettes = 0x08303678, shinyPalettes = 0x08304438, icons = 0x0857BCA8, iconPalIdx = 0x0857C388, iconPals = 0x0857C540, rmTiles = 0x0859F77C, rmTilemap = 0x085A04E0, rmPal = 0x0859F73C, rmCursor = 0x0859F60C, rmCursorPal = 0x0859F5EC, rmHero = 0x085A084C, rmHeroPal = 0x085A082C, rmHeroine = 0x085A08EC, rmHeroinePal = 0x085A08CC, navDotsTiles = 0x0861FC98, navDotsMap = 0x0861FCAC, navDotsPal = 0x0861FC78, navDevTiles = 0x0861FD6C, navDevMap = 0x0861FFF4, navDevPal = 0x0861FD4C })
add("BPRE0", { game = "FIRERED", rng = 0x03005000, party = 0x02024284, enemy = 0x0202402C, sb1ptr = 0x03005008, sb2ptr = 0x0300500C, mapHeader = 0x02036DFC, main = 0x030030F0, battleType = 0x02022B4C, opponent = 0x020386AE, battleMons = 0x02023BE4, battleStructPtr = 0x02023FE8, partyIdx = 0x02023BCE, speciesNames = 0x08245EE0, abilityNames = 0x0824FC40, natureNames = 0x08463E60, speciesInfo = 0x08254784, trainers = 0x0823EAC8, classNames = 0x0823E558, mapNames = 0x083F1CAC, seviiMapsecs = 0x083F1AA4, mapGroups = 0x083526A8, frontPics = 0x082350AC, palettes = 0x0823730C, shinyPalettes = 0x082380CC, icons = 0x083D37A0, iconPalIdx = 0x083D3E80, iconPals = 0x083D4038, rmTiles = 0x083EF61C, rmPal = 0x083EF2DC, rmKanto = 0x083F089C, rmSevii123 = 0x083F0AFC, rmSevii45 = 0x083F0C0C, rmSevii67 = 0x083F0CF0, rmCursor = 0x083EF4E0, rmCursorPal = 0x083EF25C, rmHero = 0x083EF524, rmHeroPal = 0x083EF27C, rmHeroine = 0x083EF59C, rmHeroinePal = 0x083EF29C, layoutKanto = 0x083F2490, layoutSevii123 = 0x083F2724, layoutSevii45 = 0x083F29B8, layoutSevii67 = 0x083F2C4C })
add("BPRE1", { game = "FIRERED", rng = 0x03005000, party = 0x02024284, enemy = 0x0202402C, sb1ptr = 0x03005008, sb2ptr = 0x0300500C, mapHeader = 0x02036DFC, main = 0x030030F0, battleType = 0x02022B4C, opponent = 0x020386AE, battleMons = 0x02023BE4, battleStructPtr = 0x02023FE8, partyIdx = 0x02023BCE, speciesNames = 0x08245F50, abilityNames = 0x0824FCB0, natureNames = 0x08463EC0, speciesInfo = 0x082547F4, trainers = 0x0823EB38, classNames = 0x0823E5C8, mapNames = 0x083F1D1C, seviiMapsecs = 0x083F1B14, mapGroups = 0x08352718, frontPics = 0x0823511C, palettes = 0x0823737C, shinyPalettes = 0x0823813C, icons = 0x083D3810, iconPalIdx = 0x083D3EF0, iconPals = 0x083D40A8, rmTiles = 0x083EF68C, rmPal = 0x083EF34C, rmKanto = 0x083F090C, rmSevii123 = 0x083F0B6C, rmSevii45 = 0x083F0C7C, rmSevii67 = 0x083F0D60, rmCursor = 0x083EF550, rmCursorPal = 0x083EF2CC, rmHero = 0x083EF594, rmHeroPal = 0x083EF2EC, rmHeroine = 0x083EF60C, rmHeroinePal = 0x083EF30C, layoutKanto = 0x083F2500, layoutSevii123 = 0x083F2794, layoutSevii45 = 0x083F2A28, layoutSevii67 = 0x083F2CBC })
add("BPGE0", { game = "LEAFGREEN", rng = 0x03005000, party = 0x02024284, enemy = 0x0202402C, sb1ptr = 0x03005008, sb2ptr = 0x0300500C, mapHeader = 0x02036DFC, main = 0x030030F0, battleType = 0x02022B4C, opponent = 0x020386AE, battleMons = 0x02023BE4, battleStructPtr = 0x02023FE8, partyIdx = 0x02023BCE, speciesNames = 0x08245EBC, abilityNames = 0x0824FC1C, natureNames = 0x08463880, speciesInfo = 0x08254760, trainers = 0x0823EAA4, classNames = 0x0823E534, mapNames = 0x083F1AE8, seviiMapsecs = 0x083F18E0, mapGroups = 0x08352688, frontPics = 0x08235088, palettes = 0x082372E8, shinyPalettes = 0x082380A8, icons = 0x083D35DC, iconPalIdx = 0x083D3CBC, iconPals = 0x083D3E74, rmTiles = 0x083EF458, rmPal = 0x083EF118, rmKanto = 0x083F06D8, rmSevii123 = 0x083F0938, rmSevii45 = 0x083F0A48, rmSevii67 = 0x083F0B2C, rmCursor = 0x083EF31C, rmCursorPal = 0x083EF098, rmHero = 0x083EF360, rmHeroPal = 0x083EF0B8, rmHeroine = 0x083EF3D8, rmHeroinePal = 0x083EF0D8, layoutKanto = 0x083F22CC, layoutSevii123 = 0x083F2560, layoutSevii45 = 0x083F27F4, layoutSevii67 = 0x083F2A88 })
add("BPGE1", { game = "LEAFGREEN", rng = 0x03005000, party = 0x02024284, enemy = 0x0202402C, sb1ptr = 0x03005008, sb2ptr = 0x0300500C, mapHeader = 0x02036DFC, main = 0x030030F0, battleType = 0x02022B4C, opponent = 0x020386AE, battleMons = 0x02023BE4, battleStructPtr = 0x02023FE8, partyIdx = 0x02023BCE, speciesNames = 0x08245F2C, abilityNames = 0x0824FC8C, natureNames = 0x084638F0, speciesInfo = 0x082547D0, trainers = 0x0823EB14, classNames = 0x0823E5A4, mapNames = 0x083F1B58, seviiMapsecs = 0x083F1950, mapGroups = 0x083526F8, frontPics = 0x082350F8, palettes = 0x08237358, shinyPalettes = 0x08238118, icons = 0x083D364C, iconPalIdx = 0x083D3D2C, iconPals = 0x083D3EE4, rmTiles = 0x083EF4C8, rmPal = 0x083EF188, rmKanto = 0x083F0748, rmSevii123 = 0x083F09A8, rmSevii45 = 0x083F0AB8, rmSevii67 = 0x083F0B9C, rmCursor = 0x083EF38C, rmCursorPal = 0x083EF108, rmHero = 0x083EF3D0, rmHeroPal = 0x083EF128, rmHeroine = 0x083EF448, rmHeroinePal = 0x083EF148, layoutKanto = 0x083F233C, layoutSevii123 = 0x083F25D0, layoutSevii45 = 0x083F2864, layoutSevii67 = 0x083F2AF8 })
add("BPEE0", { game = "EMERALD", build = "QoL", custom = true, rng = 0x03005DA0, party = 0x020244EC, enemy = 0x02024744, sb1ptr = 0x03005DAC, sb2ptr = 0x03005DB0, mapHeader = 0x02037320, main = 0x030022E0, battleType = 0x02022FEC, opponent = 0x02038BD2, battleMons = 0x02024084, battleStructPtr = 0x0202449C, partyIdx = 0x0202406E, speciesNames = 0x0831B524, abilityNames = 0x0831E637, natureNames = 0x086202A4, speciesInfo = 0x08323548, trainers = 0x08312F8C, classNames = 0x08312C30, mapEntries = 0x085A47B8, mapGroups = 0x084896F4, frontPics = 0x0830D0E8, palettes = 0x083065D4, shinyPalettes = 0x08307394, icons = 0x0857EE5C, iconPalIdx = 0x0857F53C, iconPals = 0x0857F6F4, rmTiles = 0x085A2AB8, rmTilemap = 0x085A381C, rmPal = 0x085A2A78, rmCursor = 0x085A2948, rmCursorPal = 0x085A2928, rmHero = 0x085A3B88, rmHeroPal = 0x085A3B68, rmHeroine = 0x085A3C28, rmHeroinePal = 0x085A3C08, navDotsTiles = 0x08623408, navDotsMap = 0x0862341C, navDotsPal = 0x086233E8, navDevTiles = 0x086234DC, navDevMap = 0x08623764, navDevPal = 0x086234BC })
add("BPRE1", { game = "FIRERED", build = "QoL", custom = true, rng = 0x03005000, party = 0x02024284, enemy = 0x0202402C, sb1ptr = 0x03005008, sb2ptr = 0x0300500C, mapHeader = 0x02036DFC, main = 0x030030F0, battleType = 0x02022B4C, opponent = 0x020386AE, battleMons = 0x02023BE4, battleStructPtr = 0x02023FE8, partyIdx = 0x02023BCE, speciesNames = 0x08246038, abilityNames = 0x0824FD98, natureNames = 0x08463E70, speciesInfo = 0x082548DC, trainers = 0x0823EC20, classNames = 0x0823E6B0, mapNames = 0x083F1DDC, seviiMapsecs = 0x083F1BD4, mapGroups = 0x08352800, frontPics = 0x08235204, palettes = 0x08237464, shinyPalettes = 0x08238224, icons = 0x083D38D0, iconPalIdx = 0x083D3FB0, iconPals = 0x083D4168, rmTiles = 0x083EF74C, rmPal = 0x083EF40C, rmKanto = 0x083F09CC, rmSevii123 = 0x083F0C2C, rmSevii45 = 0x083F0D3C, rmSevii67 = 0x083F0E20, rmCursor = 0x083EF610, rmCursorPal = 0x083EF38C, rmHero = 0x083EF654, rmHeroPal = 0x083EF3AC, rmHeroine = 0x083EF6CC, rmHeroinePal = 0x083EF3CC, layoutKanto = 0x083F25C0, layoutSevii123 = 0x083F2854, layoutSevii45 = 0x083F2AE8, layoutSevii67 = 0x083F2D7C })
add("BPGE1", { game = "LEAFGREEN", build = "QoL", custom = true, rng = 0x03005000, party = 0x02024284, enemy = 0x0202402C, sb1ptr = 0x03005008, sb2ptr = 0x0300500C, mapHeader = 0x02036DFC, main = 0x030030F0, battleType = 0x02022B4C, opponent = 0x020386AE, battleMons = 0x02023BE4, battleStructPtr = 0x02023FE8, partyIdx = 0x02023BCE, speciesNames = 0x08246018, abilityNames = 0x0824FD78, natureNames = 0x084638A4, speciesInfo = 0x082548BC, trainers = 0x0823EC00, classNames = 0x0823E690, mapNames = 0x083F1C1C, seviiMapsecs = 0x083F1A14, mapGroups = 0x083527E4, frontPics = 0x082351E4, palettes = 0x08237444, shinyPalettes = 0x08238204, icons = 0x083D3710, iconPalIdx = 0x083D3DF0, iconPals = 0x083D3FA8, rmTiles = 0x083EF58C, rmPal = 0x083EF24C, rmKanto = 0x083F080C, rmSevii123 = 0x083F0A6C, rmSevii45 = 0x083F0B7C, rmSevii67 = 0x083F0C60, rmCursor = 0x083EF450, rmCursorPal = 0x083EF1CC, rmHero = 0x083EF494, rmHeroPal = 0x083EF1EC, rmHeroine = 0x083EF50C, rmHeroinePal = 0x083EF20C, layoutKanto = 0x083F2400, layoutSevii123 = 0x083F2694, layoutSevii45 = 0x083F2928, layoutSevii67 = 0x083F2BBC })

-- The addresses of your own build, from a .sym file next to the ROM (filled in by GB-Link Companion)
--@OVERRIDE@

-- Per-game layout differences that the symbol files don't give.
local FAMILY = {
  RUBY      = { primary = 512, primaryPals = 6, flags = 0x1220, hiddenStart = 0x258, inBattle = 0x43D, mapsecNone = 0x58, gridW = 28, gridH = 15, region = "HOENN" },
  SAPPHIRE  = { primary = 512, primaryPals = 6, flags = 0x1220, hiddenStart = 0x258, inBattle = 0x43D, mapsecNone = 0x58, gridW = 28, gridH = 15, region = "HOENN" },
  EMERALD   = { primary = 512, primaryPals = 6, flags = 0x1270, hiddenStart = 0x1F4, inBattle = 0x439, mapsecNone = 0xD5, gridW = 28, gridH = 15, region = "HOENN" },
  FIRERED   = { primary = 640, primaryPals = 7, flags = 0xEE0, hiddenStart = 1000, inBattle = 0x439, mapsecNone = 0xC5, gridW = 22, gridH = 15, region = "KANTO", frlg = true },
  LEAFGREEN = { primary = 640, primaryPals = 7, flags = 0xEE0, hiddenStart = 1000, inBattle = 0x439, mapsecNone = 0xC5, gridW = 22, gridH = 15, region = "KANTO", frlg = true },
}
local MAPSEC_DYNAMIC = 0x57
local KANTO_MAPSEC_START = 0x58

local V, F, gameName = nil, nil, nil     -- this game's addresses, family, and name for the screen
local unsupported = nil                  -- why the status screens can't work with this game

local function r8(a) return emu:read8(a) end
local function r16(a) return emu:read16(a) end
local function r32(a) return emu:read32(a) end

-- the games' English character set
local CHARS = {}
do
  local function span(first, s) for i = 1, #s do CHARS[first + i - 1] = s:sub(i, i) end end
  CHARS[0x00] = " "
  span(0xA1, "0123456789!?.-")
  span(0xB1, "\"\"''MF,")          -- B5/B6 are the male/female signs: shown as M / F
  CHARS[0xB0] = "..."
  CHARS[0xB8] = ","
  CHARS[0xBA] = "/"
  span(0xBB, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz")
  CHARS[0xF0] = ":"
  CHARS[0x1B] = "e"                -- e acute (POKEMON)
  CHARS[0x5C] = "("
  CHARS[0x5D] = ")"
  CHARS[0x2D] = "&"
  CHARS[0x34] = "Lv"
  CHARS[0x35] = "="
  CHARS[0x53] = "PK"
  CHARS[0x54] = "MN"
end

-- a 0xFF-terminated game string at addr (at most max bytes)
local function gstr(addr, max)
  local out = {}
  local i = 0
  while i < max do
    local c = r8(addr + i)
    if c == 0xFF then break end
    if c == 0xFC then i = i + 1                                -- text colour codes: skip
    elseif c == 0xFD then                                       -- {PLAYER} / {RIVAL} placeholders
      local p = r8(addr + i + 1)
      out[#out + 1] = (p == 1 and "PLAYER") or (p == 6 and "RIVAL") or ""
      i = i + 1
    else
      out[#out + 1] = CHARS[c] or "?"
    end
    i = i + 1
  end
  return table.concat(out)
end

local function encode(s)          -- ASCII to the games' charset (for checking a ROM string)
  local t = {}
  for i = 1, #s do
    local c = s:sub(i, i)
    for k, v in pairs(CHARS) do if v == c and k >= 0xA1 then t[#t + 1] = k; break end end
  end
  return t
end

-- does this ROM have its data where build v says? (species 1's name, nature 0's name)
local function matches(v)
  local function at(addr, text)
    local want = encode(text)
    for i, b in ipairs(want) do if r8(addr + i - 1) ~= b then return false end end
    return r8(addr + #want) == 0xFF
  end
  local nat = r32(v.natureNames)
  return at(v.speciesNames + 11, "BULBASAUR") and nat >= 0x08000000 and nat < 0x0A000000 and at(nat, "HARDY")
end

local function detect()
  local code = ""
  for i = 0, 3 do code = code .. string.char(r8(0x080000AC + i)) end
  local rev = r8(0x080000BC)
  local builds = {}
  if OVERRIDE and OVERRIDE.code == code then builds[1] = OVERRIDE end   -- a .sym next to the ROM
  for _, v in ipairs(VERSIONS[code .. rev] or {}) do builds[#builds + 1] = v end
  if #builds == 0 then
    unsupported = "This game isn't supported: only Pokemon Ruby, Sapphire, Emerald, FireRed and " ..
                  "LeafGreen (English). Its code is " .. code .. ", revision " .. rev .. "."
    return
  end
  for _, v in ipairs(builds) do
    if matches(v) then
      V = v
      F = FAMILY[V.game]
      gameName = V.game .. (rev > 0 and (" v1." .. rev) or "") .. (V.build and (" " .. V.build) or "")
      return
    end
  end
  unsupported = "This looks like a modified " .. builds[#builds].game .. " (ROM hack): its data isn't " ..
                "where the original game keeps it. Put the build's .sym file next to the ROM (same name)."
end

----------------------------------------------------------------------------------- names
local cache = { species = {}, ability = {}, nature = {}, mapsec = {} }
local function speciesName(s)
  if s == 0 or s > 450 then return "?" end
  local n = cache.species[s]
  if not n then n = gstr(V.speciesNames + 11 * s, 11); cache.species[s] = n end
  return n
end
local function abilityName(a)
  local n = cache.ability[a]
  if not n then n = gstr(V.abilityNames + 13 * a, 13); cache.ability[a] = n end
  return n
end
local NATURE_STATS = { "Atk", "Def", "Spe", "SpA", "SpD" }
local function natureName(n)
  local s = cache.nature[n]
  if not s then s = gstr(r32(V.natureNames + 4 * n), 12); cache.nature[n] = s end
  return s
end
local function natureEffect(n)
  local up, down = n // 5, n % 5
  if up == down then return "neutral" end
  return "+" .. NATURE_STATS[up + 1] .. " -" .. NATURE_STATS[down + 1]
end
local function mapsecName(m)
  local n = cache.mapsec[m]
  if n then return n end
  if F.frlg then
    if m >= KANTO_MAPSEC_START and m < KANTO_MAPSEC_START + 109 then
      n = gstr(r32(V.mapNames + 4 * (m - KANTO_MAPSEC_START)), 24)
    else n = "" end
  elseif m < F.mapsecNone then
    local p = r32(V.mapEntries + 8 * m + 4)
    n = gstr(p, 24)
    -- Ruby/Sapphire store town names as "LITTLEROOT" <end> " TOWN": put the two parts together
    local i = 0
    while i < 24 and r8(p + i) ~= 0xFF do i = i + 1 end
    if r8(p + i) == 0xFF and r8(p + i + 1) == 0x00 then n = n .. gstr(p + i + 1, 12) end
  else
    n = ""
  end
  cache.mapsec[m] = n
  return n
end

------------------------------------------------------------------------------- Pokemon data
-- For personality % 24: where the growth, attacks, EVs and misc blocks are (0-3).
local ORDER = {
  [0] = {0,1,2,3}, {0,1,3,2}, {0,2,1,3}, {0,3,1,2}, {0,2,3,1}, {0,3,2,1},
  {1,0,2,3}, {1,0,3,2}, {2,0,1,3}, {3,0,1,2}, {2,0,3,1}, {3,0,2,1},
  {1,2,0,3}, {1,3,0,2}, {2,1,0,3}, {3,1,0,2}, {2,3,0,1}, {3,2,0,1},
  {1,2,3,0}, {1,3,2,0}, {2,1,3,0}, {3,1,2,0}, {2,3,1,0}, {3,2,1,0},
}

-- Reads the Pokemon at addr (a party slot). nil if the slot is empty or the data is being changed.
local function readMon(addr)
  local pid, otid = r32(addr), r32(addr + 4)
  local key = pid ~ otid
  local words, sum = {}, 0
  for i = 0, 11 do
    local w = r32(addr + 32 + 4 * i) ~ key
    words[i] = w
    sum = sum + (w & 0xFFFF) + (w >> 16)
  end
  if (sum & 0xFFFF) ~= r16(addr + 28) then return nil end
  local o = ORDER[pid % 24]
  local growth, misc = 3 * o[1], 3 * o[4]
  local species = words[growth] & 0xFFFF
  if species == 0 or species > 440 then return nil end
  local ivw = words[misc + 1]
  local m = {
    pid = pid, otid = otid, species = species,
    isEgg = (ivw >> 30) & 1 == 1,
    ivs = { hp = ivw & 31, atk = (ivw >> 5) & 31, def = (ivw >> 10) & 31,
            spe = (ivw >> 15) & 31, spa = (ivw >> 20) & 31, spd = (ivw >> 25) & 31 },
    abilityNum = (ivw >> 31) & 1,
    status = r32(addr + 80), level = r8(addr + 84), hp = r16(addr + 86), maxHP = r16(addr + 88),
  }
  local info = V.speciesInfo + 28 * species
  m.catchRate = r8(info + 8)
  m.genderRatio = r8(info + 16)
  local a1, a2 = r8(info + 22), r8(info + 23)
  m.ability = (m.abilityNum == 1 and a2 ~= 0) and a2 or a1
  m.nature = pid % 25
  m.shiny = ((otid >> 16) ~ (otid & 0xFFFF) ~ (pid >> 16) ~ (pid & 0xFFFF)) < 8
  if m.genderRatio == 255 then m.gender = "-"
  elseif m.genderRatio == 254 then m.gender = "F"
  elseif m.genderRatio == 0 then m.gender = "M"
  else m.gender = ((pid & 0xFF) < m.genderRatio) and "F" or "M" end
  return m
end

local function statusName(s)
  if s & 7 ~= 0 then return "Asleep" end
  if s & 0x88 ~= 0 then return "Poisoned" end
  if s & 0x10 ~= 0 then return "Burned" end
  if s & 0x20 ~= 0 then return "Frozen" end
  if s & 0x40 ~= 0 then return "Paralyzed" end
  return "OK"
end

local function isqrt(n)
  local r = math.floor(math.sqrt(n))
  while r * r > n do r = r - 1 end
  while (r + 1) * (r + 1) <= n do r = r + 1 end
  return r
end

-- The chance (0-1) that a ball catches it, worked out the way the games do (Cmd_handleballthrow).
local function catchChance(rate, ballX10, hp, maxHP, status)
  if maxHP == 0 then return 0 end
  local odds = (rate * ballX10 // 10) * (maxHP * 3 - hp * 2) // (3 * maxHP)
  if status & 0x27 ~= 0 then odds = odds * 2                 -- asleep or frozen
  elseif status & 0xD8 ~= 0 then odds = odds * 15 // 10 end  -- poisoned, burned or paralyzed
  if odds > 254 then return 1, odds end
  if odds <= 0 then return 0, 0 end
  local b = 1048560 // isqrt(isqrt(16711680 // odds))
  local p = math.min(b, 65536) / 65536
  return p ^ 4, odds
end

------------------------------------------------------------------------------------- RNG
local A, C = 0x41C64E6D, 0x00006073
local JA, JC = {}, {}                      -- the LCRNG jumped by 2^i steps
do
  local a, c = A, C
  for i = 0, 31 do
    JA[i], JC[i] = a, c
    c = (c * (a + 1)) & 0xFFFFFFFF
    a = (a * a) & 0xFFFFFFFF
  end
end
-- How many advances take seed `from` to seed `to` (exact, mod 2^32).
local function distance(from, to)
  local d, s = 0, from
  for i = 0, 31 do
    local bit = 1 << i
    if (s ~ to) & bit ~= 0 then
      s = (JA[i] * s + JC[i]) & 0xFFFFFFFF
      d = d | bit
    end
  end
  return d
end

local shinyHunt            -- below
local findOrigin           -- below
local wildFrameLine        -- below
local rng = { prev = nil, origin = nil, start = nil, searching = nil, target = nil, best = nil, bestD = nil,
              lastDelta = 0, rate = 0, rateFrames = 0, rateAdv = 0 }
-- A 16-bit seed this close behind the current one can't be there by chance (65536 candidates in
-- 2^32 seeds: on average the nearest wrong one is 65536 advances away).
local ORIGIN_SURE = 8000

-- The games seed the RNG with a 16-bit value (from the clock or a timer), so the initial seed is
-- the 16-bit seed closest behind the current one. Searched a slice per frame so mGBA doesn't stall.
local function searchOrigin()
  local s = rng.searching
  for _ = 1, 512 do                  -- about 2 seconds for all 65536, without slowing mGBA
    local d = distance(s, rng.target)
    if not rng.bestD or d < rng.bestD then rng.best, rng.bestD = s, d end
    s = s + 1
    if s > 0xFFFF then
      if rng.bestD <= ORIGIN_SURE then rng.origin = rng.best end
      rng.searching = nil
      return
    end
  end
  rng.searching = s
end

local function startSearch(cur)
  rng.searching, rng.target, rng.best, rng.bestD, rng.origin = 0, cur, nil, nil, nil
end

-- The next shiny frame: the frame (counted like "Advances") whose next two RNG calls make a shiny
-- personality for your Trainer ID and Secret ID: low half from the first call, high half from
-- the second (how the games make a static, gift or starter Pokemon). Searched ahead a slice at a
-- time; once the frame passes, the next one is found.
local hunt = nil
local function trainerXor()
  local sb2 = V.sb2 or r32(V.sb2ptr)
  if sb2 < 0x02000000 or sb2 >= 0x03008000 then return nil end
  local tid, sid = r16(sb2 + 0x0A), r16(sb2 + 0x0C)
  if tid == 0 and sid == 0 then return nil end      -- no save loaded yet
  return tid ~ sid
end
local function nextSeed(x) return (A * x + C) & 0xFFFFFFFF end

shinyHunt = function(cur)
  local tsv = trainerXor()
  local base = rng.origin or rng.start
  if not tsv or not base then hunt = nil; return end
  local now = distance(base, cur)
  if not hunt or hunt.base ~= base or hunt.tsv ~= tsv or (hunt.found and hunt.found <= now) or
     (not hunt.found and hunt.frame <= now) then
    hunt = { base = base, tsv = tsv, frame = now + 1, seed = nextSeed(cur), found = nil }
  end
  if hunt.found then return end
  local f, x = hunt.frame, hunt.seed
  for _ = 1, 4000 do
    local r1 = nextSeed(x)
    local r2 = nextSeed(r1)
    local lo, hi = r1 >> 16, r2 >> 16
    if (tsv ~ lo ~ hi) < 8 then
      local r3 = nextSeed(r2)                  -- then the IVs, from the next two calls
      local r4 = nextSeed(r3)
      local a, b = r3 >> 16, r4 >> 16
      hunt.found, hunt.pid = f, (hi << 16) | lo
      hunt.ivs = { a & 31, (a >> 5) & 31, (a >> 10) & 31, (b >> 5) & 31, (b >> 10) & 31, b & 31 }  -- HP Atk Def SpA SpD Spe
      break
    end
    f, x = f + 1, r1
  end
  hunt.frame, hunt.seed = f, x
end

-- The frame a wild Pokemon came from: step the RNG back from now until the two calls that made
-- its personality (low half, then high half). A slice a frame, up to 4 million advances back.
local AINV, CINV = 0xEEB9EB65, 0x0A3561A1
local origin = { pid = nil }
findOrigin = function(pid)
  if V.custom then return nil end
  if origin.pid ~= pid then
    local cur = r32(V.rng)
    origin = { pid = pid, x = cur, steps = 0, frame = nil, done = false }
  end
  if origin.done then return origin.frame end
  local lo, hi = pid & 0xFFFF, pid >> 16
  local x, nx = origin.x, origin.nx
  for _ = 1, 20000 do
    local px = (AINV * x + CINV) & 0xFFFFFFFF
    if nx and (x >> 16) == lo and (nx >> 16) == hi then      -- x made the low half, nx the high
      local base = rng.origin or rng.start
      origin.frame, origin.done = base and distance(base, px) or nil, true
      return origin.frame
    end
    nx, x = x, px
    origin.steps = origin.steps + 1
    if origin.steps > 4000000 then origin.done = true; return nil end
  end
  origin.x, origin.nx = x, nx
  return nil
end

local function rngFrame()
  local cur = r32(V.rng)
  if rng.prev then
    local d = distance(rng.prev, cur)
    if d > 50000 then                     -- reseeded: find the new starting seed
      startSearch(cur)
      rng.start = cur
      d = 0
    end
    rng.lastDelta = d
    rng.rateFrames, rng.rateAdv = rng.rateFrames + 1, rng.rateAdv + d
    if rng.rateFrames >= 60 then
      rng.rate, rng.rateFrames, rng.rateAdv = rng.rateAdv / rng.rateFrames, 0, 0
    end
  else
    startSearch(cur)
    rng.start = cur
  end
  rng.prev = cur
  if rng.searching then searchOrigin() end
  shinyHunt(cur)
end

------------------------------------------------------------------------------- sprites
-- Pokemon pictures, read from the game in mGBA: front sprites (LZ77-compressed 64x64, 4 bits a
-- pixel, normal or shiny palette) and party icons (32x32). Sent as hex: 16 colours, then tiles.

-- the GBA BIOS's LZ77 (type 0x10): returns at most `limit` bytes
local function lz77(addr, limit)
  local hdr = r32(addr)
  if hdr & 0xFF ~= 0x10 then return nil end
  local size = math.min(hdr >> 8, limit)
  local out, n, src = {}, 0, addr + 4
  while n < size do
    local flags = r8(src); src = src + 1
    for bit = 7, 0, -1 do
      if n >= size then break end
      if flags & (1 << bit) == 0 then
        n = n + 1; out[n] = r8(src); src = src + 1
      else
        local b1, b2 = r8(src), r8(src + 1); src = src + 2
        local len, disp = (b1 >> 4) + 3, ((b1 & 0xF) << 8 | b2) + 1
        for _ = 1, len do
          if n >= size then break end
          n = n + 1; out[n] = out[n - disp] or 0
        end
      end
    end
  end
  return out
end

local function hexBytes(t, first, count)
  local parts = {}
  for i = first, first + count - 1 do parts[#parts + 1] = string.format("%02x", t[i] or 0) end
  return table.concat(parts)
end

local function romBytes(addr, n)
  local t = {}
  for i = 1, n do t[i] = r8(addr + i - 1) end
  return t
end

local function isRom(a) return a >= 0x08000000 and a < 0x0A000000 end

local spriteCache = {}
-- species' front sprite as hex (palette + 64 tiles), or nil
local function frontSprite(species, shiny)
  local key = "f" .. species .. (shiny and "s" or "")
  if spriteCache[key] ~= nil then return spriteCache[key] or nil end
  local result = false
  local pic = r32(V.frontPics + 8 * species)
  local pal = r32((shiny and V.shinyPalettes or V.palettes) + 8 * species)
  if isRom(pic) and isRom(pal) then
    local tiles = lz77(pic, 2048)
    local colors = lz77(pal, 32)
    if tiles and colors and #tiles == 2048 and #colors == 32 then
      result = hexBytes(colors, 1, 32) .. hexBytes(tiles, 1, 2048)
    end
  end
  spriteCache[key] = result
  return result or nil
end

-- species' party icon as hex (palette + 16 tiles), or nil
local function iconSprite(species)
  local key = "i" .. species
  if spriteCache[key] ~= nil then return spriteCache[key] or nil end
  local result = false
  local tiles = r32(V.icons + 4 * species)
  local idx = r8(V.iconPalIdx + species)
  local pal = idx < 6 and r32(V.iconPals + 8 * idx) or 0
  if isRom(tiles) and isRom(pal) then
    result = hexBytes(romBytes(pal, 32), 1, 32) .. hexBytes(romBytes(tiles, 512), 1, 512)
  end
  spriteCache[key] = result
  return result or nil
end

-- a picture on a screen: at text column `col`, top at the screen's line number `line`
local function spriteSpec(slot, col, line, w, h, hex)
  if not hex then return nil end
  return "S" .. slot .. "," .. col .. "," .. line .. "," .. w .. "," .. h .. "," .. hex
end

----------------------------------------------------------------------------------- screens
-- A screen is a list of lines; each line starts with its colour:
-- 0 white, 1 grey, 2 green, 3 red, 4 yellow, 5 cyan.
local W = 40
local function pad(s, n) s = tostring(s); if #s >= n then return s:sub(1, n) end return s .. string.rep(" ", n - #s) end
local function rpad(s, n) s = tostring(s); if #s >= n then return s:sub(1, n) end return string.rep(" ", n - #s) .. s end
local function title(left)
  local right = gameName or ""
  return "5" .. pad(left, W - #right - 1) .. " " .. right
end
local function commas(n)
  local s = tostring(math.floor(n))
  local out = s:reverse():gsub("(%d%d%d)", "%1,"):reverse()
  return (out:gsub("^,", ""))
end
local function hex(v, n) return string.format("%0" .. n .. "X", v) end

local function inBattle() return (r8(V.main + F.inBattle) & 2) ~= 0 end
local function battleFlags() return r32(V.battleType) end

local function rngScreen()
  if V.custom then return { title("RNG STATE"), "1", "1Not shown for this build." } end
  local cur = r32(V.rng)
  local L = { title("RNG STATE"), "1" }
  L[#L + 1] = "0Current seed    " .. hex(cur, 8)
  if rng.origin then
    L[#L + 1] = "0Initial seed    " .. hex(rng.origin, 4)
    L[#L + 1] = "0Advances        " .. commas(distance(rng.origin, cur))
  elseif rng.searching then
    L[#L + 1] = "1Initial seed    finding it..."
    L[#L + 1] = "1Advances        -"
  else
    L[#L + 1] = "1Initial seed    unknown (seeded too long"
    L[#L + 1] = "1                before this script started)"
    L[#L + 1] = "0Advances        " .. commas(distance(rng.start, cur)) .. " since the script started"
  end
  local now = distance(rng.origin or rng.start, cur)
  if hunt and hunt.found then
    local left = hunt.found - now
    L[#L + 1] = (left <= 600 and "4" or "2") .. "Next shiny      " .. pad(commas(hunt.found), 12) .. "in " .. commas(left)
    L[#L + 1] = "0                " .. natureName(hunt.pid % 25) .. "  PID " .. hex(hunt.pid, 8)
    local iv = hunt.ivs
    L[#L + 1] = ((iv[1] + iv[2] + iv[3] + iv[4] + iv[5] + iv[6] == 186) and "2" or "1") ..
                "                IV " .. table.concat(iv, "/")
  elseif hunt then
    L[#L + 1] = "1Next shiny      searching..."
    L[#L + 1] = "1"
    L[#L + 1] = "1"
  else
    L[#L + 1] = "1Next shiny      load a save first"
    L[#L + 1] = "1"
    L[#L + 1] = "1"
  end
  L[#L + 1] = "0Last frame      +" .. rng.lastDelta
  L[#L + 1] = "0Per frame       " .. string.format("%.2f", rng.rate) .. "  (last second)"
  L[#L + 1] = "0Frames          " .. commas(r32(V.main + 0x24)) .. "  (game's counter)"
  L[#L + 1] = "1"
  L[#L + 1] = paused and "4mGBA is paused" or "1"
  L[#L + 1] = "1Frames count the advances from the seed."
  return L
end

local function ivLines(L, m, indent)
  local iv = m.ivs
  L[#L + 1] = "1" .. indent .. "IV  HP Atk Def SpA SpD Spe"
  local function c(v) return rpad(v, 3) end
  local perfect = iv.hp + iv.atk + iv.def + iv.spa + iv.spd + iv.spe == 186
  L[#L + 1] = (perfect and "2" or "0") .. indent .. "   " .. c(iv.hp) .. " " .. c(iv.atk) .. " " .. c(iv.def) ..
              " " .. c(iv.spa) .. " " .. c(iv.spd) .. " " .. c(iv.spe)
end

-- species, nature, ability and IVs: screen lines 3-8, beside the front sprite (29 columns)
local function monHead(L, m)
  local head = speciesName(m.species) .. " Lv" .. m.level .. " " .. m.gender
  L[#L + 1] = (m.shiny and "4" or "0") .. head .. (m.shiny and " SHINY" or "")
  L[#L + 1] = "0" .. natureName(m.nature) .. " (" .. natureEffect(m.nature) .. ")"
  L[#L + 1] = "0" .. abilityName(m.ability)
  ivLines(L, m, "")
end

-- "Frame 3,620" (the advance it was made on), on the line below the sprite
wildFrameLine = function(L, m)
  if V.custom then L[#L + 1] = "1"; return end
  local f = findOrigin(m.pid)
  if f then L[#L + 1] = "5Frame " .. commas(f) .. "  (the advance it was made on)"
  elseif origin.done then L[#L + 1] = "1Frame: not found (made too long ago)"
  else L[#L + 1] = "1Frame: finding it..." end
end

local lastWild, wildSafari = nil, false
local safariScreen          -- below
local function wildScreen()
  local L = { title("WILD POKEMON") }
  local live = inBattle() and (battleFlags() & 0x8) == 0
  local m = live and readMon(V.enemy) or nil
  if m then lastWild = m end
  m = m or lastWild
  if not m then
    L[#L + 1] = "1"
    L[#L + 1] = "1No wild Pokemon yet."
    L[#L + 1] = "1It shows here as soon as one appears."
    return L
  end
  local hp, maxHP, status = m.hp, m.maxHP, m.status
  if live then             -- the battler has the up-to-date HP and status
    local b = V.battleMons + 0x58
    if r16(b) == m.species then hp, maxHP, status = r16(b + 0x28), r16(b + 0x2C), r32(b + 0x4C) end
  end
  local safari = (battleFlags() & 0x80) ~= 0
  if live then wildSafari = safari end
  if wildSafari then
    L[1] = title("SAFARI POKEMON")
    return safariScreen(L, m, live)
  end
  L[#L + 1] = live and "2In battle" or "1Last one seen (not in battle now)"
  monHead(L, m)
  L[#L + 1] = "0HP " .. hp .. "/" .. maxHP .. "  " .. statusName(status)
  L[#L + 1] = "1PID " .. hex(m.pid, 8)
  wildFrameLine(L, m)
  -- the catch rate the game uses for this throw: base rate x ball, scaled by HP left, x2 asleep or
  -- frozen, x1.5 poisoned, burned or paralyzed (then shake checks turn it into a chance)
  local _, now = catchChance(m.catchRate, 10, hp, maxHP, status)
  local why = {}
  if maxHP > 0 then why[#why + 1] = "HP " .. (hp * 100 // maxHP) .. "%" end
  if status & 0x27 ~= 0 then why[#why + 1] = statusName(status):lower() .. " x2"
  elseif status & 0xD8 ~= 0 then why[#why + 1] = statusName(status):lower() .. " x1.5" end
  L[#L + 1] = "0Catch rate " .. m.catchRate .. " -> " .. now .. " right now"
  L[#L + 1] = "1  (" .. table.concat(why, ", ") .. (now > 254 and "; 255+ = sure catch)" or ")")
  local function pct(p)
    p = p * 100
    if p >= 99.95 then return "100%" end
    return string.format("%.1f%%", p)
  end
  local function ball(x) return pct((catchChance(m.catchRate, x, hp, maxHP, status))) end
  L[#L + 1] = "5 Poke " .. pad(ball(10), 7) .. "Great " .. pad(ball(15), 7) .. "Ultra " .. ball(20)
  L[#L + 1] = "1 Chance of a catch per throw, right now"
  return L, { spriteSpec(0, 29, 3, 64, 64, frontSprite(m.species, m.shiny)) }
end

-- The Safari Zone's catch and flee numbers (gBattleStruct: safari counters, flee and catch factors).
local function safariState()
  local base
  if V.battleStructPtr then base = r32(V.battleStructPtr) + 0x79 else base = 0x02016086 end  -- RS: fixed
  if base < 0x02000000 or base >= 0x02040000 then return nil end
  return { c1 = r8(base), c2 = r8(base + 1), escape = r8(base + 2), catch = r8(base + 3) }
end

safariScreen = function(L, m, live)
  local s = live and safariState() or nil
  L[#L + 1] = live and "2In the Safari Zone" or "1Last one seen (not in battle now)"
  monHead(L, m)
  L[#L + 1] = "1PID " .. hex(m.pid, 8)
  wildFrameLine(L, m)
  local spr = { spriteSpec(0, 29, 3, 64, 64, frontSprite(m.species, m.shiny)) }
  if not s then
    L[#L + 1] = "0Catch rate " .. m.catchRate
    return L, spr
  end
  -- what the Safari Ball uses: catch factor x 1275 / 100 as the catch rate, x1.5 for the ball
  local rate = (s.catch * 1275 // 100) & 0xFF
  local chance = (catchChance(rate, 15, 1, 1, 0))
  local flee
  if F.frlg then          -- FireRed/LeafGreen: a rock doubles it, bait quarters it, while they last
    if s.c1 > 0 then flee = math.min(s.escape * 2, 20)
    elseif s.c2 > 0 then flee = math.max(s.escape // 4, 1)
    else flee = s.escape end
  else
    flee = s.escape
  end
  flee = math.min(flee * 5, 100)
  local function pct(p) if p >= 0.9995 then return "100%" end return string.format("%.1f%%", p * 100) end
  L[#L + 1] = "5Safari Ball  " .. pct(chance) .. "   (catch factor " .. s.catch .. ")"
  L[#L + 1] = (flee >= 50 and "3" or "4") .. "Flee chance  " .. flee .. "% each turn   (factor " .. s.escape .. ")"
  if F.frlg then
    L[#L + 1] = "1Rock: " .. (s.c1 > 0 and (s.c1 .. " turns left") or "none") ..
                "   Bait: " .. (s.c2 > 0 and (s.c2 .. " turns left") or "none")
  else
    L[#L + 1] = "1Went near " .. s.c1 .. " times   Pokeblocks thrown " .. s.c2
  end
  return L, spr
end

local lastTrainer = nil
local function trainerScreen()
  local L = { title("TRAINER'S TEAM") }
  local live = inBattle() and (battleFlags() & 0x8) ~= 0
  local mons = nil
  if live then
    mons = {}
    for i = 0, 5 do mons[#mons + 1] = readMon(V.enemy + 100 * i) or false end
    local opp = r16(V.opponent)
    local t = V.trainers + 40 * opp
    local name = gstr(t + 4, 12)
    local class = gstr(V.classNames + 13 * r8(t + 1), 13)
    local out = {}
    for i = 0, 3 do out[i] = r8(V.partyIdx + 2 * i) end   -- party slots of the opponent battlers
    lastTrainer = { name = (class .. " " .. name):gsub("^%s+", ""), mons = mons,
                    out = { [out[1]] = true, [((battleFlags() & 1) ~= 0) and out[3] or -1] = true } }
  end
  local t = lastTrainer
  if not t then
    L[#L + 1] = "1"
    L[#L + 1] = "1No trainer battle yet."
    L[#L + 1] = "1The whole team shows here, including"
    L[#L + 1] = "1Pokemon that haven't been sent out."
    return L
  end
  local n = 0
  for _, m in ipairs(t.mons) do if m then n = n + 1 end end
  L[#L + 1] = (live and "2" or "1") .. pad(t.name, 28) .. rpad(n .. " Pokemon", 12)
  if not live then L[#L + 1] = "1(last trainer battle)" end
  local sprites = {}
  for i, m in ipairs(t.mons) do
    if m then                -- 4 lines each (36 px), with the party icon beside them
      local fainted = m.hp == 0
      local col = fainted and "1" or (m.shiny and "4" or "0")
      local tag = fainted and "fainted" or (t.out[i - 1] and live and "OUT" or "")
      sprites[#sprites + 1] = spriteSpec(i, 34, #L + 1, 32, 32, iconSprite(m.species))
      L[#L + 1] = col .. i .. " " .. pad(speciesName(m.species), 11) .. pad("Lv" .. m.level, 6) .. m.gender ..
                  (m.shiny and " SHINY" or "") .. "  " .. tag
      L[#L + 1] = "0  " .. pad(natureName(m.nature), 9) .. abilityName(m.ability)
      L[#L + 1] = "0  HP " .. pad(m.hp .. "/" .. m.maxHP, 9) .. "PID " .. hex(m.pid, 8)
      local iv = m.ivs
      L[#L + 1] = "1  IV " .. iv.hp .. "/" .. iv.atk .. "/" .. iv.def .. "/" .. iv.spa .. "/" .. iv.spd .. "/" .. iv.spe
    end
  end
  L[#L + 1] = "1IV: HP/Atk/Def/SpA/SpD/Spe"
  return L, sprites
end

-- The region map as a grid of cells: 0 empty, 1 route, 2 town/city, 3 other place, 4 where you are.
local function cellType(name)
  if name:find("^ROUTE") then return 1 end
  if name:find("TOWN$") or name:find("CITY$") or name:find("ISLAND$") or name:find("PLATEAU$") then return 2 end
  if name == "" then return 0 end
  return 3
end

local gridCache = {}
local function hoennGrid()
  if gridCache.hoenn then return gridCache.hoenn end
  local g, rects, sec = {}, {}, {}
  for i = 0, F.gridW * F.gridH - 1 do g[i] = 0 end
  for m = 0, MAPSEC_DYNAMIC - 1 do
    local e = V.mapEntries + 8 * m
    local x, y, w, h = r8(e), r8(e + 1), r8(e + 2), r8(e + 3)
    if w > 0 and h > 0 and w <= F.gridW and h <= F.gridH then
      rects[m] = { x = x, y = y, w = w, h = h }
    end
  end
  for pass = 1, 3 do          -- routes, then places, then towns on top
    for m, r in pairs(rects) do
      local t = cellType(mapsecName(m))
      if (pass == 1 and t == 1) or (pass == 2 and t == 3) or (pass == 3 and t == 2) then
        for yy = r.y, r.y + r.h - 1 do
          for xx = r.x, r.x + r.w - 1 do
            if xx < F.gridW and yy < F.gridH then g[yy * F.gridW + xx] = t; sec[yy * F.gridW + xx] = m end
          end
        end
      end
    end
  end
  gridCache.hoenn = { cells = g, rects = rects, sec = sec }
  return gridCache.hoenn
end

local FRLG_LAYOUTS = { "layoutKanto", "layoutSevii123", "layoutSevii45", "layoutSevii67" }
local FRLG_REGIONS = { "KANTO", "ONE-THREE ISLAND", "FOUR-FIVE ISLAND", "SIX-SEVEN ISLAND" }
local function frlgRegion(mapsec)
  for r = 1, 3 do
    for i = 0, 29 do
      local m = r8(V.seviiMapsecs + 30 * (r - 1) + i)
      if m == F.mapsecNone then break end
      if m == mapsec then return r + 1 end
    end
  end
  return 1
end
local function frlgGrid(region)
  local key = "frlg" .. region
  if gridCache[key] then return gridCache[key] end
  local base = V[FRLG_LAYOUTS[region]]
  local g, where, sec = {}, {}, {}
  for layer = 1, 0, -1 do        -- layer 1: dungeons, drawn under layer 0 (towns and routes)
    for y = 0, F.gridH - 1 do
      for x = 0, F.gridW - 1 do
        local m = r8(base + layer * F.gridW * F.gridH + y * F.gridW + x)
        local i = y * F.gridW + x
        if m ~= F.mapsecNone then
          local t = cellType(mapsecName(m))
          if layer == 0 or (g[i] or 0) == 0 then g[i] = t; sec[i] = m end
          where[m] = where[m] or {}
          table.insert(where[m], { x = x, y = y })
        else
          g[i] = g[i] or 0
        end
      end
    end
  end
  gridCache[key] = { cells = g, where = where, sec = sec }
  return gridCache[key]
end

-- Trainers and hidden items of every place (map section), from every map's events: trainers are
-- the people whose script starts a trainer battle (trainerbattle, 0x5C), hidden items are the
-- hidden-item background events. Built once; whether each is done comes from the save's flags.
local TRAINER_FLAGS_START = 0x500
local mapIndex = nil

local function findTrainer(script, isTrainer)
  if script < 0x08000000 or script >= 0x0A000000 then return nil end
  for j = 0, isTrainer and 40 or 0 do          -- trainers: a few checks may come first (FRLG)
    if r8(script + j) == 0x5C then
      local kind, id = r8(script + j + 1), r16(script + j + 2)
      if kind <= 8 and id > 0 and id < 1000 then return id end
    end
  end
  return nil
end

local function buildIndex()
  local idx = {}
  local function place(m)
    idx[m] = idx[m] or { trainers = {}, items = {}, maps = {} }
    return idx[m]
  end
  local groups, tbl = {}, V.mapGroups
  for i = 0, 63 do
    local p = r32(tbl + 4 * i)
    if p < 0x08000000 or p >= tbl then break end
    groups[#groups + 1] = p
  end
  for _, g in ipairs(groups) do
    local stop = tbl                           -- a group's list ends where the next one starts
    for _, o in ipairs(groups) do if o > g and o < stop then stop = o end end
    for a = g, stop - 4, 4 do
      local hdr = r32(a)
      local ev = isRom(hdr) and r32(hdr + 4) or 0
      if isRom(ev) then
        local p = place(r8(hdr + 0x14))
        local lay = r32(hdr)
        if isRom(lay) then table.insert(p.maps, { layout = lay, type = r8(hdr + 0x17) }) end
        local objs, bgs = r32(ev + 4), r32(ev + 16)
        for k = 0, r8(ev) - 1 do
          local o = objs + 24 * k
          if not F.frlg or r8(o + 2) == 0 then    -- FRLG: skip clones of objects on next maps
            local id = findTrainer(r32(o + 0x10), r16(o + 0x0C) ~= 0)
            if id then p.trainers[id] = true end
          end
        end
        for k = 0, r8(ev + 3) - 1 do
          local b = bgs + 12 * k
          if r8(b + 5) == 7 then                 -- BG_EVENT_HIDDEN_ITEM
            local flag = F.frlg and ((r32(b + 8) >> 16) & 0xFF) + F.hiddenStart or r16(b + 0x0A) + F.hiddenStart
            p.items[flag] = true
          end
        end
      end
    end
  end
  return idx
end

local function flagSet(flag)
  local sb1 = V.sb1 or r32(V.sb1ptr)
  return (r8(sb1 + F.flags + (flag >> 3)) >> (flag & 7)) & 1 == 1
end

-- "Trainers: 3/7  Items: 2/5" (left to do / all) for a place
local function placeCounts(m)
  if not V.mapGroups then return "1" end
  mapIndex = mapIndex or buildIndex()
  local p = mapIndex[m]
  local tl, tt, il, it = 0, 0, 0, 0
  if p then
    for id in pairs(p.trainers) do tt = tt + 1; if not flagSet(TRAINER_FLAGS_START + id) then tl = tl + 1 end end
    for f in pairs(p.items) do it = it + 1; if not flagSet(f) then il = il + 1 end end
  end
  local function part(l, t) return t == 0 and "-" or (l .. "/" .. t) end
  local col = (tl + il > 0) and "0" or "2"
  return col .. "Trainers: " .. part(tl, tt) .. "  Items: " .. part(il, it)
end

-- A picture of a place: its main map (the biggest outdoor one, else the biggest), one colour per
-- 16x16 metatile, averaged from the game's own tiles and palettes. Sent as hex colours.
local tileStats, metaColor, tilesetBytes, previewCache = {}, {}, {}, {}

local function tilesOf(ts)                     -- the tileset's tile data (decompressed if need be)
  if tilesetBytes[ts] == nil then
    local ptr = r32(ts + 4)
    if r8(ts) ~= 0 then tilesetBytes[ts] = lz77(ptr, 0x8000) or false
    else tilesetBytes[ts] = { rom = ptr } end
  end
  return tilesetBytes[ts]
end

-- how many pixels of a tile use each colour (0-15)
local function tileHist(ts, n)
  local key = ts * 1024 + n
  local h = tileStats[key]
  if h then return h end
  h = {}
  for c = 0, 15 do h[c] = 0 end
  local t = tilesOf(ts)
  if t then
    for i = 0, 31 do
      local b = t.rom and r8(t.rom + n * 32 + i) or (t[n * 32 + i + 1] or 0)
      h[b & 15] = h[b & 15] + 1
      h[b >> 4] = h[b >> 4] + 1
    end
  else
    h[0] = 64
  end
  tileStats[key] = h
  return h
end

local function rgb(c) return c & 31, (c >> 5) & 31, (c >> 10) & 31 end

-- a metatile's average colour as r, g, b (0-31)
local function metatile(prim, sec, id)
  local key = prim .. ":" .. sec .. ":" .. id
  local m = metaColor[key]
  if m then return m end
  local ts, i = prim, id
  if id >= F.primary then ts, i = sec, id - F.primary end
  local refs = r32(ts + 12) + 16 * i
  local bgR, bgG, bgB = rgb(r16(r32(prim + 8)))  -- colour 0 of palette 0: the backdrop
  local R, G, B = 0, 0, 0
  for q = 0, 3 do
    local col = { bgR, bgG, bgB }
    for layer = 0, 1 do
      local ref = r16(refs + 2 * (layer * 4 + q))
      local tile, pal = ref & 0x3FF, ref >> 12
      local tts, tn = prim, tile
      if tile >= F.primary then tts, tn = sec, tile - F.primary end
      local palBase = r32((pal < F.primaryPals and prim or sec) + 8) + 32 * pal
      local h = tileHist(tts, tn)
      local n, r, g, b = 0, 0, 0, 0
      for c = 1, 15 do
        if h[c] > 0 then
          local cr, cg, cb = rgb(r16(palBase + 2 * c))
          r, g, b, n = r + cr * h[c], g + cg * h[c], b + cb * h[c], n + h[c]
        end
      end
      if n > 0 then                              -- see-through pixels show what's below
        local k = n / 64
        col[1] = col[1] * (1 - k) + r / n * k
        col[2] = col[2] * (1 - k) + g / n * k
        col[3] = col[3] * (1 - k) + b / n * k
      end
    end
    R, G, B = R + col[1], G + col[2], B + col[3]
  end
  m = { R / 4, G / 4, B / 4 }
  metaColor[key] = m
  return m
end

local PREVIEW_W, PREVIEW_H = 240, 124
local function placePicture(m)
  if previewCache[m] ~= nil then return previewCache[m] or nil end
  mapIndex = mapIndex or buildIndex()
  local p = mapIndex[m]
  local best, bestScore = nil, -1
  for _, mp in ipairs(p and p.maps or {}) do
    local w, h = r32(mp.layout), r32(mp.layout + 4)
    if w > 0 and h > 0 and w < 512 and h < 512 then
      local outdoor = mp.type == 1 or mp.type == 2 or mp.type == 3 or mp.type == 5 or mp.type == 6
      local score = w * h + (outdoor and 1000000 or 0)
      if score > bestScore then best, bestScore = mp.layout, score end
    end
  end
  if not best then previewCache[m] = false; return nil end
  local w, h = r32(best), r32(best + 4)
  local blocks, prim, sec = r32(best + 12), r32(best + 16), r32(best + 20)
  if not (isRom(blocks) and isRom(prim) and isRom(sec)) then previewCache[m] = false; return nil end
  local k = math.max(1, math.ceil(w / PREVIEW_W), math.ceil(h / PREVIEW_H))   -- big maps: k x k blocks a dot
  local ow, oh = math.ceil(w / k), math.ceil(h / k)
  local out = {}
  for oy = 0, oh - 1 do
    for ox = 0, ow - 1 do
      local R, G, B, n = 0, 0, 0, 0
      for y = oy * k, math.min(h, oy * k + k) - 1 do
        for x = ox * k, math.min(w, ox * k + k) - 1 do
          local c = metatile(prim, sec, r16(blocks + 2 * (y * w + x)) & 0x3FF)
          R, G, B, n = R + c[1], G + c[2], B + c[3], n + 1
        end
      end
      local v = math.floor(R / n + 0.5) | math.floor(G / n + 0.5) << 5 | math.floor(B / n + 0.5) << 10
      out[#out + 1] = string.format("%04x", v)
    end
  end
  local spec = "P" .. ow .. "," .. oh .. "," .. table.concat(out)
  previewCache[m] = spec
  return spec
end

-- The game's own region map: the background (tiles + tilemap + palette), the player's icon and
-- the cursor. Sent once each: B<region> (an indexed picture: colours, then a byte a pixel) and
-- I<name> (16x16 icons, like sprites). The picture covers the screen below the 3 text lines
-- (y 32-159), laid out so the map cells sit where the GBA's map cells are.
local bgCache, iconCache = {}, {}
local function rgbHex(t, first, n)                -- n colours from a byte table, as hex
  return hexBytes(t, first, 2 * n)
end

local REGION_TILEMAP = { KANTO = "rmKanto", ["ONE-THREE ISLAND"] = "rmSevii123",
                         ["FOUR-FIVE ISLAND"] = "rmSevii45", ["SIX-SEVEN ISLAND"] = "rmSevii67" }

local function regionBackground(region)
  if bgCache[region] ~= nil then return bgCache[region] or nil end
  local W, H = 240, 128
  local out, pal = {}, nil
  if not F.frlg then
    -- RSE: an 8bpp affine background, 64 tiles wide, colours 0x70-0x9F from its palette; the
    -- map cells start at (8, 16) on the game's screen, (8, 32) here
    local tiles, map = lz77(V.rmTiles or 0, 0x8000), lz77(V.rmTilemap or 0, 0x1000)
    if not tiles or not map then bgCache[region] = false; return nil end
    pal = {}
    for i = 0, 255 do pal[i] = 0 end
    for i = 0, 47 do pal[0x70 + i] = r16(V.rmPal + 2 * i) end
    pal[0] = pal[0x70]
    for y = 0, H - 1 do
      local my = y + 16
      for x = 0, W - 1 do
        local t = map[(my >> 3) * 64 + (x >> 3) + 1] or 0
        out[#out + 1] = tiles[t * 64 + (my & 7) * 8 + (x & 7) + 1] or 0
      end
    end
  else
    -- FRLG: a 4bpp text background, 30x20 tiles, 16-colour palettes; the map cells start at
    -- (32, 32) on the game's screen and here
    local key = REGION_TILEMAP[region] or "rmKanto"
    local tiles, map = lz77(V.rmTiles or 0, 0x10000), lz77(V[key] or 0, 1200)
    if not tiles or not map then bgCache[region] = false; return nil end
    pal = {}
    for i = 0, 255 do pal[i] = i < 80 and r16(V.rmPal + 2 * i) or 0 end
    for y = 0, H - 1 do
      local sy = y + 32
      for x = 0, W - 1 do
        local i = ((sy >> 3) * 30 + (x >> 3)) * 2 + 1
        local e = (map[i] or 0) | (map[i + 1] or 0) << 8
        local t, hf, vf, p = e & 0x3FF, e & 0x400 ~= 0, e & 0x800 ~= 0, e >> 12
        local px, py = x & 7, sy & 7
        if hf then px = 7 - px end
        if vf then py = 7 - py end
        local b = tiles[t * 32 + py * 4 + (px >> 1) + 1] or 0
        local c = (px & 1 == 1) and (b >> 4) or (b & 15)
        out[#out + 1] = c == 0 and 0 or (p * 16 + c)
      end
    end
  end
  local palHex = {}
  for i = 0, 255 do palHex[#palHex + 1] = string.format("%04x", pal[i] & 0x7FFF) end
  local px = {}
  for i = 1, #out do px[i] = string.format("%02x", out[i]) end
  local spec = "B" .. region .. "\t" .. W .. "," .. H .. "," .. table.concat(palHex) .. "," .. table.concat(px)
  bgCache[region] = spec
  return spec
end

-- The PokeNav look for the GBA's screens: POKENAV (the menu: the dotted background with the
-- device outline over it) and PAGE (the other screens: just the dots). From the game where it has
-- them (Emerald: both; Ruby/Sapphire: the outline), else a plain dotted page drawn here (FireRed
-- and LeafGreen have no PokeNav). 240x160, colours 0-15 the dots, 16-31 the outline.
local function navLayer(tilesPtr, mapPtr, base, first)
  if not tilesPtr or not mapPtr then return nil end
  local tiles = lz77(tilesPtr, 0x4000) or romBytes(tilesPtr, 0x400)
  local map = lz77(mapPtr, 0x800)
  if not tiles or not map then return nil end
  return { tiles = tiles, map = map, base = base, first = first or 0 }
end

local DOT_COLOURS = { [0] = 0x7FBD, [1] = 0x737B }        -- made here: a pale page with fine dots

local function pokenavPicture(key)
  if bgCache[key] ~= nil then return bgCache[key] or nil end
  local dots = navLayer(V.navDotsTiles, V.navDotsMap, 0)
  -- (Ruby/Sapphire load the outline's tiles one tile in)
  local dev = key == "POKENAV" and navLayer(V.navDevTiles, V.navDevMap, 16, F.region == "HOENN" and not V.navDotsTiles and 1 or 0) or nil
  local out = {}
  for y = 0, 159 do
    for x = 0, 239 do
      local v = 0
      if not dots and (x % 4 == 1) and (y % 4 == 1) then v = 1 end
      for _, l in ipairs({ dots or false, dev or false }) do if l then  -- the outline goes over the dots
        local i = ((y >> 3) * 32 + (x >> 3)) * 2 + 1
        local e = (l.map[i] or 0) | (l.map[i + 1] or 0) << 8
        local t, px, py = (e & 0x3FF) - l.first, x & 7, y & 7
        if e & 0x400 ~= 0 then px = 7 - px end
        if e & 0x800 ~= 0 then py = 7 - py end
        local b = t >= 0 and l.tiles[t * 32 + py * 4 + (px >> 1) + 1] or 0
        local c = (px & 1 == 1) and (b >> 4) or (b & 15)
        if c ~= 0 then v = l.base + c end
      end end
      out[#out + 1] = string.format("%02x", v)
    end
  end
  local palHex = {}
  for i = 0, 255 do
    local c = 0
    if dots and i < 16 then c = r16(V.navDotsPal + 2 * (i == 0 and 1 or i))   -- under it all: the dots' first colour
    elseif not dots and i < 2 then c = DOT_COLOURS[i]
    elseif dev and i >= 16 and i < 32 then c = r16(V.navDevPal + 2 * (i - 16)) end
    palHex[#palHex + 1] = string.format("%04x", c & 0x7FFF)
  end
  local spec = "B" .. key .. "\t240,160," .. table.concat(palHex) .. "," .. table.concat(out)
  bgCache[key] = spec
  return spec
end

-- a 16x16 icon: palette (16 colours) + 4 tiles, compressed or not
local function icon(gfx, palAddr, compressed)
  if not gfx or not palAddr then return nil end
  local key = gfx .. ":" .. palAddr
  if iconCache[key] ~= nil then return iconCache[key] or nil end
  local tiles = compressed and lz77(gfx, 128) or romBytes(gfx, 128)
  if not tiles or #tiles < 128 then iconCache[key] = false; return nil end
  local spec = hexBytes(romBytes(palAddr, 32), 1, 32) .. hexBytes(tiles, 1, 128)
  iconCache[key] = spec
  return spec
end

local function playerIcon()
  local sb2 = V.sb2 or r32(V.sb2ptr)
  local girl = sb2 >= 0x02000000 and sb2 < 0x03008000 and r8(sb2 + 8) == 1
  if girl then return icon(V.rmHeroine, V.rmHeroinePal, F.frlg) end
  return icon(V.rmHero, V.rmHeroPal, F.frlg)
end

local MAP_TYPES = { [1] = "town", [2] = "city", [3] = "route", [4] = "underground", [5] = "underwater",
                    [8] = "indoors", [9] = "secret base" }

local function mapScreen()
  local sb1 = V.sb1 or r32(V.sb1ptr)
  local px, py = r16(sb1) , r16(sb1 + 2)
  if px >= 0x8000 then px = px - 0x10000 end
  if py >= 0x8000 then py = py - 0x10000 end
  local mapsec = r8(V.mapHeader + 0x14)
  local mapType = r8(V.mapHeader + 0x17)
  local layout = r32(V.mapHeader)
  local mw, mh = 1, 1
  if layout >= 0x08000000 and layout < 0x0A000000 then mw, mh = r32(layout), r32(layout + 4) end
  local name = mapsec ~= MAPSEC_DYNAMIC and mapsecName(mapsec) or ""
  local outdoors = mapType == 1 or mapType == 2 or mapType == 3 or mapType == 5 or mapType == 6
  local region, cells, cx, cy = F.region, nil, nil, nil
  local function scale(v, size, n) if size <= 0 then return 0 end local c = v * n // size; return math.max(0, math.min(n - 1, c)) end
  if F.frlg then
    local r = frlgRegion(mapsec)
    region = FRLG_REGIONS[r]
    local grid = frlgGrid(r)
    cells = {}
    for i = 0, F.gridW * F.gridH - 1 do cells[i] = grid.cells[i] end
    local list = grid.where[mapsec]
    if list then
      local x0, y0, x1, y1 = 99, 99, -1, -1
      for _, c in ipairs(list) do
        cells[c.y * F.gridW + c.x] = 4
        x0, y0, x1, y1 = math.min(x0, c.x), math.min(y0, c.y), math.max(x1, c.x), math.max(y1, c.y)
      end
      local tx, ty = x0 + (x1 - x0) // 2, y0 + (y1 - y0) // 2
      if outdoors then tx, ty = x0 + scale(px, mw, x1 - x0 + 1), y0 + scale(py, mh, y1 - y0 + 1) end
      local best, bd = list[1], 1e9          -- the nearest cell that is part of this place
      for _, c in ipairs(list) do
        local d = (c.x - tx) ^ 2 + (c.y - ty) ^ 2
        if d < bd then best, bd = c, d end
      end
      cx, cy = best.x, best.y
    end
  else
    local grid = hoennGrid()
    cells = {}
    for i = 0, F.gridW * F.gridH - 1 do cells[i] = grid.cells[i] end
    local r = grid.rects[mapsec]
    if r then
      for yy = r.y, r.y + r.h - 1 do
        for xx = r.x, r.x + r.w - 1 do
          if xx < F.gridW and yy < F.gridH then cells[yy * F.gridW + xx] = 4 end
        end
      end
      cx, cy = r.x + (r.w - 1) // 2, r.y + (r.h - 1) // 2
      if outdoors then cx, cy = r.x + scale(px, mw, r.w), r.y + scale(py, mh, r.h) end
    end
  end
  local L = { title("MAP  " .. region) }
  local grid = F.frlg and frlgGrid(frlgRegion(mapsec)) or hoennGrid()
  local sel = cursor and grid.sec[cursor[2] * F.gridW + cursor[1]] or nil
  if cursor then                 -- looking around: the place under the cursor
    for i = 0, F.gridW * F.gridH - 1 do
      if cells[i] == 4 then cells[i] = grid.cells[i] end
      if sel and grid.sec[i] == sel then cells[i] = 4 end
    end
    if sel then
      L[#L + 1] = "4" .. mapsecName(sel)
      L[#L + 1] = placeCounts(sel)
    else
      L[#L + 1] = "1Nothing here"
      L[#L + 1] = "1"
    end
  else
    local here = (name ~= "" and name or "Somewhere unnamed") .. (MAP_TYPES[mapType] and not outdoors and (" (" .. MAP_TYPES[mapType] .. ")") or "")
    L[#L + 1] = (name ~= "" and "4" or "1") .. pad(here, 26) .. rpad("X" .. px .. " Y" .. py, 14)
    L[#L + 1] = name ~= "" and placeCounts(mapsec) or "1"
  end
  local digits = {}
  for i = 0, F.gridW * F.gridH - 1 do digits[#digits + 1] = tostring(cells[i]) end
  local g = "G" .. F.gridW .. "," .. F.gridH .. "," .. (cx or 255) .. "," .. (cy or 255) .. "," .. table.concat(digits)
  local parts = { g }
  local okBg, bg = pcall(regionBackground, region)
  if okBg and bg then
    sendOnce("bg:" .. region, bg)
    parts[#parts + 1] = "R" .. region
    local hero, cur = playerIcon(), icon(V.rmCursor, V.rmCursorPal, true)
    if hero then parts[#parts + 1] = "Ihero," .. hero end
    if cur then parts[#parts + 1] = "Icursor," .. cur end
  end
  if preview and sel then
    local pic = placePicture(sel)
    if pic then parts[#parts + 1] = pic; return L, parts end
    L[3] = "1No picture for this place"
  end
  return L, parts
end

local SCREENS = { rngScreen, wildScreen, trainerScreen, mapScreen }

----------------------------------------------------------------------------- the link
local function apply(k)
  if not emu then return end
  local up = added & ~k
  if up ~= 0 then emu:clearKeys(up) end
  if k ~= 0 then emu:addKeys(k) end
  added = k
end

local function drop(why)
  if sock then sock:close() end
  sock, buf, tries = nil, "", 0
  apply(0)
  if why then console:log("GB-Link: " .. why) end
end

local function send(line)
  if not sock then return end
  local data, i = line .. "\n", 1
  while i <= #data do                  -- send() can take only part of a long line
    local last = sock:send(data, i)
    if not last then drop("disconnected"); return end
    if last < i then return end
    i = last + 1
  end
end

local lastSent, frame, helloSent = nil, 0, false
local sentOnce = {}           -- big things the app keeps (region map pictures): sent once a connection
function sendOnce(key, line)
  if sentOnce[key] then return end
  send(line)
  sentOnce[key] = true
end

local function connect()
  if tries >= 5 then return end
  wait = wait - 1
  if wait > 0 then return end
  wait = 60                                     -- about once a second
  tries = tries + 1
  local s = socket.tcp()
  if s:connect("127.0.0.1", PORT) then
    sock, buf, lastSent, helloSent, sentOnce = s, "", nil, false, {}
    console:log("GB-Link: connected - the GBA's buttons control mGBA, and its menu shows game info")
  else
    s:close()
    if tries >= 5 then
      console:log("GB-Link: GB-Link Companion isn't running. Load this script again to retry.")
    end
  end
end

local readCommands, afterCommands, pauseHere   -- below

local function sendScreen(L, extra)
  local parts = { "D" .. mode }
  for _, l in ipairs(L) do parts[#parts + 1] = (l:gsub("[\t\r\n]", " ")) end
  for _, x in pairs(extra or {}) do parts[#parts + 1] = x end     -- map, sprites
  local line = table.concat(parts, "\t")
  if line ~= lastSent then send(line); lastSent = line end
end

local function poll()
  if not sock then connect(); return end
  while true do
    local ready, err = sock:hasdata()
    if err then drop("disconnected"); return end
    if not ready then break end
    local data = sock:receive(512)
    if not data then drop("disconnected"); return end
    buf = buf .. data
  end
  local keys = readCommands()
  buf = buf:match("[^\n]*$")
  if keys then quiet = 0 else quiet = quiet + 1 end
  if quiet > 45 and added ~= 0 and holdFrames == 0 then
    apply(0)                             -- no word from the GBA for a while (saving): let go
  end
  if holdFrames > 0 then                 -- just went on: the chosen buttons for a few frames
    holdFrames = holdFrames - 1
    apply(((keys or added) | holdKeys) & 0x3FF)
    if holdFrames == 0 then apply(keys or 0) end
  elseif keys then apply(keys & 0x3FF) end
  afterCommands()
end

readCommands = function()
  local keys = nil
  for line in buf:gmatch("([^\n]*)\n") do
    local c = line:sub(1, 1)
    if c == "P" then
      local op, arg = line:sub(2, 2), line:sub(3)
      if op == "P" then paused, steps = true, 0
      elseif op == "S" then steps = steps + (tonumber(arg) or 1)
      elseif op == "G" then
        paused, steps = false, 0
        holdKeys, holdFrames = (tonumber(arg, 16) or 0) & 0x3FF, (tonumber(arg, 16) or 0) ~= 0 and HOLD_FRAMES or 0
      end
      urgent = true
    elseif c == "K" then keys = tonumber(line:sub(2), 16)
    elseif c == "M" then mode = tonumber(line:sub(2)) or 0; lastSent = nil
    elseif c == "C" then
      local x, y, v = line:match("^C(%d+),(%d+),?(%d*)")
      cursor = x and { tonumber(x), tonumber(y) } or nil
      preview = v == "1"
      urgent = true
    elseif line:match("^%x+$") then keys = tonumber(line, 16) end     -- older app versions
  end
  return keys
end

-- A wild Pokemon appeared: tell the app (for its encounter counter), once a battle.
local wasWild = false
local function watchEncounters()
  local wild = inBattle() and (battleFlags() & 0x0A) == 0      -- not a trainer or link battle
  if wild and not wasWild then
    local m = readMon(V.enemy)
    if m then
      send("E" .. speciesName(m.species) .. "\t" .. m.level .. "\t" .. (m.shiny and 1 or 0))
      wasWild = true
    end
  elseif not wild then
    wasWild = false
  end
end

afterCommands = function()

  if not emu then return end
  if not V and not unsupported then detect() end
  if not helloSent then
    send("H" .. (V and gameName or ("!" .. (unsupported or "No game"))) .. ((V and V.custom) and "\t-rng" or ""))
    helloSent = true
  end
  if not V then return end
  if not V.custom then rngFrame() end   -- own builds (QoL): no RNG screen
  if false and not sentOnce.nav then       -- the PokeNav look for the GBA's screens (off: too slow)
    sentOnce.nav = true
    for _, key in ipairs({ "POKENAV", "PAGE" }) do
      local okN, pic = pcall(pokenavPicture, key)
      if okN and pic then send(pic) end
    end
  end
  pcall(watchEncounters)
  frame = frame + 1
  if mode >= 1 and mode <= #SCREENS and (frame % 10 == 0 or urgent) then
    urgent = false
    local ok, L, extra = pcall(SCREENS[mode])
    if not ok then L, extra = { "3Couldn't read the game: " .. tostring(L):sub(1, 60) }, nil end
    sendScreen(L, extra)
  end
  if paused then pauseHere() end
end

-- Paused: hold mGBA here, between frames, until a step or "go" arrives (or the app goes away).
pauseHere = function()
  if steps > 0 then steps = steps - 1; return end
  if mode >= 1 and mode <= #SCREENS then         -- where it stopped, on the GBA
    local okS, L, extra = pcall(SCREENS[mode])
    if okS then sendScreen(L, extra) end
  end
  while paused and steps == 0 and sock do
    local ok = sock._s:select(20)              -- wait up to 20 ms for the app
    if ok and ok < 0 then drop("disconnected"); break end
    if ok and ok > 0 then
      local data = sock:receive(512)
      if not data then drop("disconnected"); break end
      buf = buf .. data
      readCommands()
      buf = buf:match("[^\n]*$")
      if mode >= 1 and mode <= #SCREENS then       -- show the change on the GBA straight away
        local okS, L, extra = pcall(SCREENS[mode])
        if okS then sendScreen(L, extra) end
      end
    end
  end
  if not sock then paused = false end
  if steps > 0 then steps = steps - 1 end
end

callbacks:add("frame", poll)
callbacks:add("reset", function() added = 0; V, unsupported, helloSent = nil, nil, false; cache = { species = {}, ability = {}, nature = {}, mapsec = {} }; gridCache = {}; spriteCache = {}; mapIndex = nil; tileStats, metaColor, tilesetBytes, previewCache = {}, {}, {}, {}; bgCache, iconCache = {}, {}; rng.prev, rng.origin, rng.searching = nil, nil, nil end)
callbacks:add("start", function() added = 0 end)
console:log("GB-Link controller and status script loaded (port " .. PORT .. ")")
