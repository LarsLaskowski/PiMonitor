<!---
Thanks for filing a pull request! Before you submit, please read the following:

Search open/closed issues before submitting. Someone may have pushed the same thing before!

Provide a summary of your changes in the title field above.
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

## 👩‍💻 Reviewer Notes

<!---
Provide some notes for reviewers to help them provide targeted feedback and testing.

Do you recommend a smoke test for this PR? What steps should be followed?
Are there particular areas of the code the reviewer should focus on?
-->

## 📑 Test Plan

<!---
Please provide a summary of the tests affected by this work and any unique strategies
employed in testing the features/fixes. See docs/TESTS.md for this project's test
conventions — tests are mandatory for new or changed behavior.
-->

## ✅ Checklist

### General

<!--- Review the list and put an x in the boxes that apply. -->

- [ ] I have added/updated [tests](../docs/TESTS.md) for my changes (`go test ./... -race -cover` passes locally).
- [ ] `go vet ./...` and `golangci-lint run` are clean.
- [ ] I have tested my changes.
- [ ] I have read the [CONTRIBUTING](../docs/CONTRIBUTING.md) documentation and followed the project's code style guidelines.
- [ ] I have updated [`ARCHITECTURE.md`](../docs/ARCHITECTURE.md) if this changes a documented design decision.

### REST API / configuration / packaging

<!--- Review the list and put an x in the boxes that apply. -->
<!--- Remove this section if not applicable. -->

- [ ] I have updated [`docs/API.md`](../docs/API.md) to reflect a REST API change.
- [ ] No breaking change to `/api/v1/...` response shapes, or a new API version
      (`/api/v2/...`) was introduced instead.
- [ ] I have updated `README.md` / `packaging/pimonitor.example.yaml` to reflect a new or
      changed configuration option.
- [ ] I have updated `packaging/install.sh` or the systemd units if this changes
      installation/packaging, and kept the unprivileged/privileged service split intact
      (see [`SECURITY.md`](../SECURITY.md)).

## ⏭ Next Steps

<!---
If there is relevant follow-up work to this PR, please list any existing issues or
provide brief descriptions of what you would like to do next.
-->
