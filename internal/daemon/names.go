package daemon

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// French first names for Claude Code agents
var frenchFirstNames = []string{
	"jacques", "pierre", "jean", "louis", "francois",
	"antoine", "henri", "michel", "philippe", "claude",
	"laurent", "olivier", "nicolas", "pascal", "rene",
	"andre", "marcel", "etienne", "lucien", "thierry",
	"yves", "alain", "xavier", "benoit", "guillaume",
	"julien", "maxime", "sebastien", "arnaud", "mathieu",
	"fabien", "cedric", "damien", "stephane", "christophe",
	"emmanuel", "frederic", "gerard", "hugues", "jerome",
	"kevin", "lionel", "marc", "norbert", "patrice",
	"quentin", "raymond", "sylvain", "tristan", "urbain",
	"valentin", "wilfried", "yannick", "zacharie", "adrien",
	"bastien", "cyril", "didier", "edouard", "florian",
	"gaston", "herve", "ismael", "joel", "kilian",
	"loic", "matthias", "noel", "octave", "paul",
	"raphael", "serge", "thibault", "ulysse", "vincent",
	"william", "yoann", "alexis", "bruno", "camille",
	"denis", "eric", "felix", "gilles", "hubert",
	"ivan", "joseph", "leo", "marius", "nathan",
	"oscar", "prosper", "regis", "samuel", "theo",
	"victor", "willy", "yanis", "aurelien", "baptiste",
}

// French last names for Claude Code agents
var frenchLastNames = []string{
	"bernard", "dubois", "moreau", "laurent", "simon",
	"michel", "lefevre", "leroy", "roux", "david",
	"bertrand", "morel", "girard", "andre", "lecomte",
	"fournier", "mercier", "dupont", "lambert", "bonnet",
	"fontaine", "rousseau", "vincent", "muller", "legrand",
	"garnier", "chevalier", "clement", "blanchard", "gauthier",
	"perrin", "robin", "masson", "sanchez", "henry",
	"duval", "denis", "lemaire", "lucas", "martinez",
	"petit", "marchand", "durand", "marie", "picard",
	"richard", "thomas", "robert", "garcia", "barbier",
	"rodriguez", "brunet", "martin", "renard", "arnaud",
	"leroux", "colin", "vidal", "dupuis", "faure",
	"guillot", "gautier", "roger", "benoit", "lacroix",
	"meyer", "hubert", "rey", "jean", "maillard",
	"baron", "boyer", "perrot", "guerin", "philippe",
	"leblanc", "carpentier", "charles", "renaud", "dumas",
	"olivier", "aubert", "pons", "brun", "gaillard",
	"noel", "louis", "pierre", "mathieu", "charpentier",
	"fabre", "moulin", "adam", "berger", "roy",
	"giraud", "leclerc", "caron", "collet", "prevost",
}

// California/Chad-style first names for Codex agents
var californiaFirstNames = []string{
	"chad", "brad", "brock", "bryce", "trent",
	"cody", "kyle", "blake", "derek", "tyler",
	"hunter", "skyler", "chase", "austin", "ryan",
	"dustin", "travis", "troy", "dillon", "colton",
	"logan", "mason", "jayden", "kayden", "cooper",
	"tucker", "walker", "parker", "tanner", "gunner",
	"bronson", "canyon", "cliff", "dallas", "denver",
	"easton", "ford", "gage", "hawk", "jace",
	"kane", "lance", "maverick", "nash", "oakley",
	"paxton", "quinn", "ryder", "sawyer", "thor",
	"wade", "zane", "ashton", "beckett", "cash",
	"dax", "easton", "finn", "grayson", "hayes",
	"jett", "knox", "levi", "maddox", "nolan",
	"phoenix", "reed", "steele", "tate", "vance",
	"weston", "xander", "yates", "zeke", "ace",
	"bodhi", "colt", "duke", "eli", "fletcher",
	"grady", "heath", "ivan", "jake", "keaton",
	"lane", "miles", "nico", "otto", "pierce",
	"reese", "shane", "trey", "urban", "vaughn",
	"wolf", "xavier", "yuma", "zander", "axel",
}

// California/Chad-style last names for Codex agents
var californiaLastNames = []string{
	"stevenson", "anderson", "peterson", "johnson", "williamson",
	"henderson", "richardson", "davidson", "morrison", "harrison",
	"thornton", "preston", "lawson", "bronson", "ashton",
	"dalton", "grayson", "winston", "clifton", "carlton",
	"bradford", "stanford", "crawford", "hartford", "stratford",
	"wellington", "bennington", "harrington", "worthington", "huntington",
	"barrington", "lexington", "remington", "covington", "paddington",
	"kensington", "livingston", "kingston", "princeton", "weston",
	"easton", "shelton", "walton", "sutton", "norton",
	"fulton", "colton", "bolton", "holton", "melton",
	"ashford", "bradford", "langford", "sanford", "radford",
	"beaumont", "claremont", "fremont", "piedmont", "belmont",
	"blackwell", "caldwell", "hartwell", "rockwell", "cromwell",
	"whitfield", "mayfield", "fairfield", "westfield", "springfield",
	"brooks", "rivers", "stone", "hill", "woods",
	"sterling", "golden", "silver", "hunter", "archer",
	"fletcher", "carter", "mason", "taylor", "cooper",
	"brewer", "fisher", "marshall", "porter", "chandler",
	"foster", "butler", "turner", "palmer", "parker",
	"sawyer", "fletcher", "spencer", "tucker", "weaver",
}

// NameGenerator generates unique human-friendly names for agents
type NameGenerator struct {
	mu       sync.Mutex
	rng      *rand.Rand
	usedNames map[string]bool
}

// NewNameGenerator creates a new name generator
func NewNameGenerator() *NameGenerator {
	return &NameGenerator{
		rng:       rand.New(rand.NewSource(time.Now().UnixNano())),
		usedNames: make(map[string]bool),
	}
}

// GenerateName generates a unique human-friendly name for the given agent type
func (ng *NameGenerator) GenerateName(agentType string) string {
	ng.mu.Lock()
	defer ng.mu.Unlock()

	var firstNames, lastNames []string

	switch agentType {
	case AgentTypeCodex:
		firstNames = californiaFirstNames
		lastNames = californiaLastNames
	default: // claude
		firstNames = frenchFirstNames
		lastNames = frenchLastNames
	}

	// Try to find an unused combination
	maxAttempts := 100
	for range maxAttempts {
		firstName := firstNames[ng.rng.Intn(len(firstNames))]
		lastName := lastNames[ng.rng.Intn(len(lastNames))]
		name := fmt.Sprintf("%s-%s", firstName, lastName)

		if !ng.usedNames[name] {
			ng.usedNames[name] = true
			return name
		}
	}

	// Fallback: add a random suffix if all combinations are exhausted
	firstName := firstNames[ng.rng.Intn(len(firstNames))]
	lastName := lastNames[ng.rng.Intn(len(lastNames))]
	suffix := ng.rng.Intn(1000)
	name := fmt.Sprintf("%s-%s-%d", firstName, lastName, suffix)
	ng.usedNames[name] = true
	return name
}

// ReleaseName marks a name as available again (when agent is killed)
func (ng *NameGenerator) ReleaseName(name string) {
	ng.mu.Lock()
	defer ng.mu.Unlock()
	delete(ng.usedNames, name)
}

// MarkUsed marks a name as in use (for recovering state)
func (ng *NameGenerator) MarkUsed(name string) {
	ng.mu.Lock()
	defer ng.mu.Unlock()
	ng.usedNames[name] = true
}
