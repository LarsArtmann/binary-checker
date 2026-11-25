# Security Policy

## Reporting a vulnerability

Report vulnerabilities privately through GitHub's
[security advisory form](https://github.com/larsartmann/binary-checker/security/advisories/new).
Do not open a public issue for a suspected vulnerability.

Include what you did, what happened, and what you expected. A minimal
reproduction (the file layout and the command you ran) is worth more than a
description.

## What this tool does with your data

`binary-checker` reads the git index and the leading bytes of tracked files. It
makes no network requests, writes no files unless you pass `--save-baseline`,
and never modifies your repository.

The one thing it reports on that can surprise people: a binary file that is
git-tracked and also matched by `.gitignore` is still reported (rule `BC003`),
because git keeps tracking files that were ignored after the fact. That is
intentional, not a false positive.

## Supported versions

The latest tagged release receives fixes. There are no long-term support
branches.
