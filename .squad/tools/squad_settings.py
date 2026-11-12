"""Per-repository settings for the squad tools (Go profile). Seeded once by adopt-template and kept on
later refreshes; the scripts that import it are template-managed."""

# Base branch the gates diff against (merge base with HEAD); fetch it before running the gates.
BASE_REF = "origin/main"

# Tier `patch` (.squad/routing.md, *Tiers*; scope-check.py --tier patch): changed lines outside specs/ at most.
PATCH_MAX_LINES = 30

# Who approves a feature's spec before it is planned (.squad/orchestration.md, *Spec approval*): "lead" or "pm".
SPEC_APPROVAL = "lead"

# Size of the squad's working records (.squad/tools/plan-check.py): words of plan.md per tier (a `Plan size:` line
# under its header allows up to twice as many), and of spec.md SPEC_BASE_WORDS plus SPEC_WORDS_PER_CRITERION per
# acceptance criterion, at most SPEC_MAX_WORDS. A feature of at most SMALL_FEATURE_CRITERIA criteria and no security
# area may let its criteria be its scenarios. Each is a ceiling for the largest change of its kind, not a target.
PLAN_MAX_WORDS = {"tests": 800, "trivial": 800, "standard": 2500, "security": 4000}
SPEC_BASE_WORDS = 300
SPEC_WORDS_PER_CRITERION = 200
SPEC_MAX_WORDS = 2500
SMALL_FEATURE_CRITERIA = 3

# Coverage gate (.squad/tools/coverage-check.py): line coverage on new/changed production lines, and overall.
# COVERAGE_OVERALL_THRESHOLD may start below COVERAGE_THRESHOLD in a repository adopted with a coverage debt;
# it is never lowered and is raised towards COVERAGE_THRESHOLD as coverage improves.
COVERAGE_THRESHOLD = 80
COVERAGE_OVERALL_THRESHOLD = 80
COVERAGE_FORMAT = "go"
COVERAGE_REPORT_GLOB = "coverage.out"
COVERAGE_PATHSPECS = ["*.go"]
COVERAGE_EXCLUDES = ["*_test.go"]

# Size warnings (config-check.py, decision-check.py): a file past its limit is reported with the split it needs.
PROJECT_MAX_WORDS = 1500  # .squad/project.md, read by five members on every launch
AREA_MAX_WORDS = 5000  # each area document in docs/areas/
RECORD_MAX_WORDS = 1500  # each unreleased decision record
RECORD_MAX_OPTIONS = 8  # the options under *Options considered* of an unreleased decision record

# Stale-figure check (.squad/tools/figure-check.py): values it does not search in the documentation (prose uses them
# for list items, steps and counts), and source files whose values it does not read (lock files, generated files).
FIGURE_TRIVIAL = ["0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "100"]
FIGURE_EXCLUDES = ["go.sum", "*.svg"]
