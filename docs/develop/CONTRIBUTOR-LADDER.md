# Contributor Ladder

This docs different ways to get involved and level up within the project. You can see different roles within the project in the contributor roles.

<!-- template begins here -->

- [Contributor Ladder](#contributor-ladder)
  - [Contributor Ladder](#contributor-ladder-1)
    - [Community Participant](#community-participant)
    - [Contributor](#contributor)
    - [Organization Member](#organization-member)
    - [Reviewer](#reviewer)
    - [Maintainer](#maintainer)
    - [An active maintainer should](#an-active-maintainer-should)
    - [How to be a maintainer](#how-to-be-a-maintainer)
    - [Removing Maintainers](#removing-maintainers)
  - [Inactivity](#inactivity)
  - [Involuntary Removal or Demotion](#involuntary-removal-or-demotion)

## Contributor Ladder

Hello! We are excited that you want to learn more about our project contributor ladder! This contributor ladder outlines the different contributor roles within the project, along with the responsibilities and privileges that come with them. Community members generally start at the first levels of the "ladder" and advance up it as their involvement in the project grows.  Our project members are happy to help you advance along the contributor ladder.

Each of the contributor roles below is organized into lists of three types of things. "Responsibilities" are things that a contributor is expected to do. "Requirements" are qualifications a person needs to meet to be in that role, and "Privileges" are things contributors on that level are entitled to.

### Community Participant

Description: A Community Participant engages with the project and its community, contributing their time, thoughts, etc. Community participants are usually users who have stopped being anonymous and started being active in project discussions.

* Responsibilities:
  * Must follow the [CNCF CoC](https://github.com/cncf/foundation/blob/main/code-of-conduct.md)
* How users can get involved with the community:
  * Participating in community discussions
  * Helping other users
  * Submitting bug reports
  * Commenting on issues
  * Trying out new releases
  * Attending community events

### Contributor

Description: A Contributor contributes directly to the project and adds value to it. Contributions need not be code. People at the Contributor level may be new contributors, or they may only contribute occasionally.

* Responsibilities include:
  * Follow the CNCF CoC
  * Follow the project contributing guide
* Requirements (one or several of the below):
  * Report and sometimes resolve issues
  * Occasionally submit PRs
  * Contribute to the documentation
  * Show up at meetings, takes notes
  * Answer questions from other community members
  * Submit feedback on issues and PRs
  * Test releases and patches and submit reviews
  * Run or helps run events
  * Promote the project in public
  * Help run the project infrastructure
  * [TODO: other small contributions]
* Privileges:
  * Invitations to contributor events
  * Eligible to become an Organization Member

A very special thanks to the [long list of people](https://github.com/Project-HAMi/HAMi/blob/master/AUTHORS) who have contributed to and helped maintain the project. We wouldn't be where we are today without your contributions. Thank you! 💖

As long as you contribute to HAMi, your name will be added [here](https://github.com/Project-HAMi/HAMi/blob/master/AUTHORS). If you don't find your name, please contact us to add it.

### Organization Member

Description: An Organization Member is an established contributor who regularly participates in the project. Organization Members have privileges in both project repositories and elections, and as such are expected to act in the interests of the whole project.

An Organization Member must meet the responsibilities and has the requirements of a Contributor, plus:

* Responsibilities include:
  * Continues to contribute regularly, as demonstrated by having at least 50 GitHub contributions per year
* Requirements:
  * Enabled [two-factor authentication] on their GitHub account
  * Must have successful contributions to the project or community, including at least one of the following:
    * 5 accepted PRs,
    * Reviewed 5 PRs,
    * Resolved and closed 3 Issues,
    * Become responsible for a key project management area,
    * Or some equivalent combination or contribution
  * Must have been contributing for at least 1 months
  * Must be actively contributing to at least one project area
  * Must have two sponsors who are also Organization Members, at least one of whom does not work for the same employer
  * **[Open an issue][membership request] against the HAMi-project/HAMi repo**
    - Ensure your sponsors are @mentioned on the issue
    - Complete every item on the issue checklist
    - Make sure that the list of contributions included is representative of your work on the project.
  * Have your sponsoring reviewers reply confirmation of sponsorship: `+1`
  * Once your sponsors have responded, your request will be handled by the `HAMi GitHub Admin team`.


* Privileges:
  * May be assigned Issues and Reviews
  * May give commands to CI/CD automation
  * Can be added to [TODO: Repo Host] teams
  * Can recommend other contributors to become Org Members

The process for a Contributor to become an Organization Member is as follows:

1. Contact Maintainers and get at least two maintainers to agree
2. Submit an Issue application to become a Member

### Reviewer

Description: A Reviewer has responsibility for specific code, documentation, test, or other project areas. They are collectively responsible, with other Reviewers, for reviewing all changes to those areas and indicating whether those changes are ready to merge. They have a track record of contribution and review in the project.

Reviewers are responsible for a "specific area." This can be a specific code directory, driver, chapter of the docs, test job, event, or other clearly-defined project component that is smaller than an entire repository or subproject. Most often it is one or a set of directories in one or more Git repositories. The "specific area" below refers to this area of responsibility.

Reviewers have all the rights and responsibilities of an Organization Member, plus:

* Responsibilities include:
  * Following the reviewing guide
  * Reviewing most Pull Requests against their specific areas of responsibility
  * Reviewing at least 20 PRs per year
  * Helping other contributors become reviewers
* Requirements:
  * Experience as a Contributor for at least 3 months
  * Is an Organization Member
  * Has reviewed, or helped review, at least 10 Pull Requests
  * Has analyzed and resolved test failures in their specific area
  * Has demonstrated an in-depth knowledge of the specific area
  * Commits to being responsible for that specific area
  * Is supportive of new and occasional contributors and helps get useful PRs in shape to commit
* Additional privileges:
  * Has GitHub or CI/CD rights to approve pull requests in specific directories
  * Can recommend and review other contributors to become Reviewers

The process of becoming a Reviewer is:

1. The contributor is nominated by opening a PR against the appropriate repository, which adds their GitHub username to the OWNERS file for one or more directories.
2. At least two members of the team that owns that repository or main directory, who are already Approvers, approve the PR.

### Maintainer

Description: Maintainers are very established contributors who are responsible for the entire project. As such, they have the ability to approve PRs against any area of the project, and are expected to participate in making decisions about the strategy and priorities of the project.

A Maintainer must meet the responsibilities and requirements of a Reviewer, plus:

The current list of maintainers can be found in the [MAINTAINERS](https://github.com/Project-HAMi/HAMi/blob/master/MAINTAINERS.md).

### An active maintainer should

* Actively participate in reviewing pull requests and incoming issues. Note that there are no hard rules on what is “active enough” and this is left up to the judgement of the current group of maintainers.

* Actively participate in discussions about design and the future of the project.

* Take responsibility for backports to appropriate branches for PRs they approve and merge.

* Do their best to follow all code, testing, and design conventions as determined by consensus among active maintainers.

* Gracefully step down from their maintainership role when they are no longer planning to actively participate in the project.

### How to be a maintainer

New maintainers are added by consensus among the current group of maintainers. This can be done via a private discussion via Slack or email. A majority of maintainers should support the addition of the new person, and no single maintainer should object to adding the new maintainer.

When adding a new maintainer, we should file a PR to [HAMi](https://github.com/Project-HAMi/HAMi) and update [MAINTAINERS](https://github.com/Project-HAMi/HAMi/blob/master/MAINTAINERS.md). Once this PR is merged, you will become a maintainer of HAMi.

### Removing Maintainers

It is normal for maintainers to come and go based on their other responsibilities. Inactive maintainers may be removed if there is no expectation of ongoing participation. If a former maintainer resumes participation, they should be given quick consideration for re-adding to the team.

## Inactivity

It is important for contributors to be and stay active to set an example and show commitment to the project. Inactivity is harmful to the project as it may lead to unexpected delays, contributor attrition, and a lost of trust in the project.

* Inactivity is measured by:
  * Periods of no contributions for longer than 3 months
  * Periods of no communication for longer than 3 months
* Consequences of being inactive include:
  * Involuntary removal or demotion
  * Being asked to move to Emeritus status

## Involuntary Removal or Demotion

Involuntary removal/demotion of a contributor happens when responsibilities and requirements aren't being met. This may include repeated patterns of inactivity, extended period of inactivity, a period of failing to meet the requirements of your role, and/or a violation of the Code of Conduct. This process is important because it protects the community and its deliverables while also opens up opportunities for new contributors to step in.

Involuntary removal or demotion is handled through a vote by a majority of the current Maintainers.

[two-factor authentication]: https://help.github.com/articles/about-two-factor-authentication
