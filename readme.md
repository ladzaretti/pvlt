# Vlt [WIP]
A command-line password manager backed by SQLite.

## TODO

- [ ] Implement all initial subcommands
  - [x] create
  - [x] login
  - [x] save
  - [ ] show
    - by --label strings, --name string, --id. + all other output related flags.
  - [ ] remove
    - by --label strings, --name string, --id. --yes/-y (alias --force/-f)
    - Print a table with the matched secrets.
      - confirm deletion from the user: delete y/N:
      - only delete if one match only. otherwise print a table and warn. exit with non 0 code.
      - Handle edge cases explicitly:
        - No match: print warning and exit with 1.
        - Multiple matches, no --yes: print and exit with 1.
        - One match, no --yes: prompt and proceed on confirmation.
        - One match, with --yes: proceed silently.
  - [x] find
    - by --label strings, --name string, --id. print to stdout.
- [ ] Add a cryptographic layer
- [ ] Add session support

searching by labels is ORed.
searching by name and labels, return the intersection between the two queries.