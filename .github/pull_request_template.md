<!---
Thanks for filing a pull request! Before you submit, please read the following:

Search open/closed issues before submitting. Someone may have pushed the same thing before!
-->

# Pull Request

## 📖 Description

<!---
Provide some background and a description of your work.
What problem does this change solve?
Is this a breaking change, chore, fix, feature, etc?
-->

## 🎫 Issues

<!---
* List and link relevant issues here, for example: Closes #123
-->

## 💬 Reviewer Notes

<!---
Provide some notes for reviewers to help them provide targeted feedback and testing.

Do you recommend a smoke test for this PR? What steps should be followed?
Are there particular areas of the code the reviewer should focus on?
-->

## 📑 Test Plan

<!---
Please provide a summary of the tests affected by this work and any unique strategies employed in testing the features/fixes.
-->

## ✅ Checklist

<!--- Review the list and put an x in the boxes that apply. -->

- [ ] I have added or updated [Unit Tests](../docs/UNIT_TESTS.md) for the change.
- [ ] I have tested my changes.
- [ ] I have run *Format* and *Build* from [`.squad/stack.md`](../.squad/stack.md), and the *Analyzer gate* reports no diagnostic in a changed file.
- [ ] The *Coverage gate* in `.squad/stack.md` passes (line coverage on new or changed production code and overall).
- [ ] I have updated the project documentation ([`README.md`](../README.md), [`ARCHITECTURE.md`](../docs/ARCHITECTURE.md)) and the [area document](../docs/areas/README.md) of every changed behavior to reflect my changes.
- [ ] Each decision with lasting weight has its [decision record](../docs/decisions/README.md), committed with the change.
- [ ] I have read the [CONTRIBUTING](../docs/CONTRIBUTING.md) documentation and followed the project's code style guidelines.
- [ ] New dependencies, if any, were added the way *Dependencies* in `.squad/stack.md` prescribes.
<!-- project:begin checklist -->
### REST API / configuration / packaging

<!--- Remove this section if not applicable. -->

- [ ] I have updated [`docs/API.md`](../docs/API.md) to reflect a REST API change.
- [ ] No breaking change to `/api/v1/...` response shapes, or a new API version
      (`/api/v2/...`) was introduced instead.
- [ ] I have updated `README.md` / `packaging/pimonitor.example.yaml` to reflect a new or
      changed configuration option.
- [ ] I have updated `packaging/install.sh` or the systemd units if this changes
      installation/packaging, and kept the unprivileged/privileged service split intact
      (see [`SECURITY.md`](../SECURITY.md)).
<!-- project:end checklist -->

## ⏭ Next Steps

<!---
If there is relevant follow-up work to this PR, please list any existing issues or provide brief descriptions of what you would like to do next.
-->
