# Chapter 32: Case Study — Replacing a Shell Script

Every project accumulates them. A script that started as four lines,
grew a second argument, then a cleanup trap, then a `|| true` on the
line that kept failing for a reason nobody looked into. It is two
hundred lines now, it is the only thing that knows how to cut a
release, and nobody wants to touch it.

This chapter takes one such script and rewrites it. Not to show that
Glide can do what bash does — it obviously can — but because the
translation is where the language's design shows up as something you
can feel rather than agree with. Almost every Glide feature in this
book exists to make one specific bash failure mode impossible, and
this is where you can see which is which.

Everything here is ✓. The program is `glide/examples/release.gld` in
the repository, and the outputs below are real.

---

### 1. Basic Usage

#### The script

A release stager. Given a version and a directory: check the version
looks like a version, check the working tree is a clean git checkout,
build a staging directory, write a manifest, checksum it, and clean up
if anything goes wrong.

Here it is in bash, in the shape these things actually reach:

```bash
#!/usr/bin/env bash
set -euo pipefail

VERSION=$1
DIR=${2:-.}

if ! [[ $VERSION =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "release: $VERSION is not a version like v1.2.3" >&2
    exit 1
fi

HEAD=$(git -C $DIR rev-parse --short HEAD 2>/dev/null) || {
    echo "release: $DIR is not a git repository" >&2
    exit 1
}

STATUS=$(git -C $DIR status --porcelain)
if [ -n "$STATUS" ]; then
    echo "release: $(echo "$STATUS" | wc -l) uncommitted file(s)" >&2
    exit 1
fi

OUT=${TMPDIR:-/tmp}/release-$VERSION
trap 'rm -rf "$OUT"' EXIT
mkdir -p $OUT

cat > $OUT/RELEASE.txt <<EOF
version $VERSION
commit  $HEAD
source  $DIR
EOF

SUM=$(shasum -a 256 $OUT/RELEASE.txt | cut -d' ' -f1)
echo "sha256  $SUM" >> $OUT/RELEASE.txt

echo "staged $VERSION in $OUT"
ls $OUT | sed 's/^/  /'
```

Thirty-eight lines. It works. It also has at least six defects, and
this chapter's claim is that the Glide version does not have them
*because of the type system*, not because I was more careful.

#### The same program in Glide

```glide
import fs
import os
import process

type ReleaseError =
    NotSemver{ given: String }
    | NotARepo{ dir: String }
    | Dirty{ files: Int }
    | ToolFailed{ tool: String, status: Int, why: String }
```

Before a line of logic: **the failure modes are a list**. Four ways
this program can fail, written down, and the compiler will not let a
`match` over them forget one. The bash script's failure modes are
`exit 1` four times, which is the same information erased.

```glide
// One conversation with one external program, in one place. `Err` is
// "the tool could not be run"; a non-zero status is the tool answering.
fn tool(name: String, args: List<String>) -> Result<String, ReleaseError> {
    let out = match process.run(name, args) {
        Ok(o)  => o
        Err(e) => { return Err(.ToolFailed{ tool: name, status: -1, why: e.message() }) }
    }
    if out.ok() == false {
        return Err(.ToolFailed{ tool: name, status: out.status(), why: out.stderr().trim() })
    }
    Ok(out.stdout().trim())
}

fn git(dir: String, args: List<String>) -> Result<String, ReleaseError> {
    let mut full = ["-C", dir]
    full.extend(args)
    tool("git", full)
}
```

`tool` is where the two things bash conflates come apart. `Err` from
`process.run` means the program could not be started — not on `PATH`,
not executable. A non-zero `status()` means it ran and said no. The
bash script cannot tell these apart at all: `set -e` treats both as
fatal, `|| true` treats both as fine, and there is no third option.

```glide
// v1.2.3 — three numeric parts after a leading v. No regex needed, and
// the failure carries what was actually given.
fn check_version(v: String) -> Result<(), ReleaseError> {
    if v.starts_with("v") == false {
        return Err(.NotSemver{ given: v })
    }
    let parts = v.trim_prefix("v").split(".")
    if parts.len() != 3 {
        return Err(.NotSemver{ given: v })
    }
    for p in parts {
        let Some(_) = p.parse_int() else { return Err(.NotSemver{ given: v }) }
    }
    Ok(())
}
```

Seven lines instead of a regex, and the error carries the offending
string rather than only announcing that one existed. There is no
`regex` module yet (○); when there is, this becomes three lines and the
error still carries `given`, because that part was never about the
regex.

```glide
fn check_tree(dir: String) -> Result<String, ReleaseError> {
    let head = match git(dir, ["rev-parse", "--short", "HEAD"]) {
        Ok(h)  => h
        Err(_) => { return Err(.NotARepo{ dir: dir }) }
    }
    let status = git(dir, ["status", "--porcelain"])?
    if status != "" {
        return Err(.Dirty{ files: status.lines().len() })
    }
    Ok(head)
}
```

Note the asymmetry, and that it is deliberate. `rev-parse` failing is
interpreted — that specific failure means "not a repository", so it is
translated into the error the caller actually wants. `status` failing
is *not* interpreted: `?` propagates whatever went wrong, because there
is no second meaning to give it.

```glide
fn stage(dir: String, version: String, head: String) -> Result<String, ReleaseError> {
    let out = fs.join([os.env("TMPDIR") ?? "/tmp", "release-{version}"])

    // If anything below fails, the half-built directory goes away.
    // A shell script needs a trap for this, and gets the signal cases
    // wrong; errdefer runs on the error path only.
    errdefer { _ = fs.remove_all(out) }

    match fs.mkdir_all(out) {
        Ok(_)  => {}
        Err(e) => { return Err(.ToolFailed{ tool: "mkdir", status: -1, why: e.message() }) }
    }

    let notes = fs.join([out, "RELEASE.txt"])
    let body = "version {version}\ncommit  {head}\nsource  {dir}\n"
    match fs.write_string(notes, body) {
        Ok(_)  => {}
        Err(e) => { return Err(.ToolFailed{ tool: "write", status: -1, why: e.message() }) }
    }

    let sum = checksum(notes)?
    match fs.append_string(notes, "sha256  {sum}\n") {
        Ok(_)  => {}
        Err(e) => { return Err(.ToolFailed{ tool: "append", status: -1, why: e.message() }) }
    }
    Ok(out)
}
```

`errdefer` is the honest version of `trap … EXIT`. The bash trap fires
on success too, which is why the script deletes its own output — see
the defects list below. `errdefer` fires on the error path only: a
`return` carrying an `Err`, what `?` propagates, or a panic.

```glide
fn explain(e: ReleaseError) -> String {
    match e {
        NotSemver{ given } => "{given} is not a version like v1.2.3"
        NotARepo{ dir }    => "{dir} is not a git repository"
        Dirty{ files }     => "{files} uncommitted file(s); commit or stash first"
        ToolFailed{ tool, status, why } =>
            if status < 0 { "{tool} could not be run: {why}" } else { "{tool} exited {status}: {why}" }
    }
}

fn run(version: String, dir: String) -> Result<String, ReleaseError> {
    check_version(version)?
    // Every child started in here dies with the scope. A shell script
    // that hangs, hangs forever.
    scope(timeout: 30.s) {
        let head = check_tree(dir)?
        stage(dir, version, head)
    } ?? Err(.ToolFailed{ tool: "git", status: -1, why: "timed out after 30s" })
}

fn main() {
    let args = os.args()
    let (version, dir) = match args {
        [_, v]    => (v, ".")
        [_, v, d] => (v, d)
        _         => {
            eprintln("usage: release <version> [dir]")
            os.exit(2)
        }
    }

    match run(version, dir) {
        Ok(out) => {
            println("staged {version} in {out}")
            for name in fs.list_dir(out) ?? [] {
                println("  {name}")
            }
            _ = fs.remove_all(out)
        }
        Err(e) => {
            eprintln("release: {explain(e)}")
            os.exit(1)
        }
    }
}
```

#### Running it

```bash
$ glide run examples/release.gld
usage: release <version> [dir]
$ echo $?
2

$ glide run examples/release.gld 1.2.3
release: 1.2.3 is not a version like v1.2.3
$ echo $?
1

$ glide run examples/release.gld v1.2.3 /tmp
release: /tmp is not a git repository

$ glide run examples/release.gld v1.2.3 .
release: 43 uncommitted file(s); commit or stash first

$ glide run examples/release.gld v1.2.3 ~/clean-checkout
staged v1.2.3 in /var/folders/…/T/release-v1.2.3
  RELEASE.txt
$ echo $?
0
```

Five paths, five distinct outcomes, and the exit codes distinguish
"you called me wrong" (2) from "something went wrong" (1).

---

### 2. Under the Hood

#### The six defects in the bash version

Each one is a real bug in the script above, and each one is
*unreachable* in the Glide version. This list is the chapter.

**1. `git -C $DIR` word-splits.** A directory named
`~/my projects/app` becomes two arguments and the script does something
else entirely. `$DIR` is unquoted in three places. In Glide,
`process.run("git", ["-C", dir, …])` passes a list; a space in `dir`
cannot become an argument boundary, because nothing is parsed.

**2. `VERSION=$1` with no argument is an error under `set -u`** — good
— but the message is `line 4: $1: unbound variable`, not a usage
string. The Glide version matches `os.args()` exhaustively and the
no-argument case is a branch with a message and exit 2.

**3. The `trap … EXIT` deletes the output on success.** Read it again:
`trap 'rm -rf "$OUT"' EXIT` fires on *every* exit, including 0. The
script prints "staged v1.2.3 in /tmp/release-v1.2.3" and then removes
the directory it just announced. To fix it you need a flag variable, or
`trap … ERR` plus the knowledge that `ERR` does not fire inside
functions unless you also set `-E`. `errdefer` has one meaning and it
is the one you wanted.

**4. `SUM=$(shasum … | cut -d' ' -f1)` swallows a missing `shasum`.**
Under `pipefail` it aborts — with the message `cut: …` or nothing at
all, depending on which half failed. The Glide version's `ToolFailed`
carries the tool name, the status and the tool's own stderr, and the
`status < 0` branch distinguishes "could not be run" from "ran and
failed".

**5. `git status --porcelain` hanging hangs the script forever.** A git
process waiting on a lock, a network filesystem, or a credential
prompt: there is no timeout, and adding one in bash means `timeout(1)`
— which is not installed on macOS — or a background subshell and a kill
loop. `scope(timeout: 30.s)` covers every child in the block, and kills
them rather than abandoning them.

**6. `echo "$STATUS" | wc -l` is off by one for empty input** and
counts wrong for filenames containing newlines. `status.lines().len()`
uses the rule from Chapter 6: split on `\n`, no phantom empty last
line.

There are two more I will not belabour: `ls $OUT | sed` breaks on
filenames with newlines, and `${TMPDIR:-/tmp}` silently uses `/tmp`
when `TMPDIR` is set to the empty string — which is exactly the
unset-versus-empty distinction `os.env`'s `Option` exists to expose.

#### What the checker did during the rewrite

Two things, both worth recording because they are the ordinary
experience rather than a highlight reel.

First, `return` inside a match arm was a parse error:

```
release.gld:24:19: expected an expression, found 'return'
 24 |         Err(e) => return Err(.ToolFailed{ … })
    |                   ^^^^^^
```

Match arms are single *expressions*, and `return` is a statement. The
fix is a block: `Err(e) => { return Err(…) }`. This is the language
being consistent rather than convenient, and it is the kind of thing
that costs ten seconds once.

Second — and this is the one that matters — adding a fifth variant to
`ReleaseError` later produces a diagnostic pointing at `explain`, with
the variant named. In bash, adding a fifth failure mode means adding a
fifth `echo … >&2; exit 1` and hoping the message is consistent with
the other four.

#### Why `?` works so freely here

Every function in the program returns `Result<_, ReleaseError>`, so `?`
never has to convert anything. That uniformity is worth designing for:
a script with one error type is a script where `?` is free.

The moment a second error type appears — say `Result<_, Error>` from
`fs` — you have three options, in increasing order of ceremony:
declare `fn from(…) -> ReleaseError` on the target so `?` converts
automatically (Chapter 20), `match` and translate as `check_tree` does,
or make the whole program use the dynamic `Error` and lose the
enumeration. The program above takes the second option deliberately,
because translating `fs` failures into `ToolFailed` is what makes the
messages read well.

---

### 3. Why This Design?

#### Why a shell script is the right thing to attack

Because shell is the language people use when the alternative is too
much ceremony, and "too much ceremony" is a design failure, not a user
failure.

Nobody writes a release script in bash because bash is good at it. They
write it in bash because `#!/usr/bin/env bash` and a text editor is the
entire setup, and the Go version needs a `go.mod`, a `main` package, a
build step and an argument parser before it prints anything.

Glide's answer is not "use a real language". It is to make a real
language have the same setup cost: one file, one command, no
build step, no manifest, and a checker that runs anyway. That is what
the interpreter tier is *for*, and it is why `DESIGN.md` insists the
interpreter ships rather than retiring at self-hosting.

#### Why the type system pays for itself in a 100-line script

The usual objection to types in scripts is that a script is too small
to be worth it. The list of six defects is the answer: every one of
them is a *type* question wearing a runtime disguise.

- Word-splitting is "is this one string or a list of strings?"
- The trap bug is "does this cleanup run on success, failure, or both?"
- The swallowed `shasum` failure is "did this produce a value or an
  error?"
- The `wc -l` bug is "what is the length of an empty list?"

A language where all four are unanswerable is a language where a
hundred-line script has six bugs, and that is not a small-program
discount — it is the reason the small program grew a `|| true`.

#### Why not just write it in Go

You could, and it would be correct. What you would also have: a
`go.mod`, a build step, `flag` or hand-rolled `os.Args` slicing,
`exec.Command` with `CombinedOutput` and a manual `ExitError` type
assertion to get the status, `errors.Is` for the failure modes because
there are no sum types, and `defer` that runs on every path so the trap
bug comes back unless you thread a `success` boolean.

The Glide version is shorter than the Go one and about the same length
as the bash one. That is the whole pitch of the scripting tier: bash's
setup cost with Go's guarantees, and a bit more of the type system than
Go has.

#### Why the errors are a sum type and not strings

Because `explain` exists. Every failure the program can produce is
rendered in one place, and adding a failure mode makes the compiler
point at that place. In the bash script the messages are scattered
across four `echo >&2` lines and there is no way to find them except
`grep`, no way to know if one is missing, and nothing stopping the
fifth one from being phrased differently.

There is a real cost, and Chapter 20 names it: a sum-type error is more
to write than `Err("something broke")`. The rule of thumb is that a
program with a *user* — even if the user is you, at 2am, reading the
output of a failed CI job — wants its failures enumerated. A program
with no user wants `Error`.

---

### 4. Competing Approaches

**bash / sh.** Universally installed, zero setup, and every value is a
string. The three flags of `set -euo pipefail` exist because the
defaults are wrong, and they are not enough: `set -e` does not fire
inside command substitutions in conditions, `pipefail` reports the
first failing stage and loses the rest, and `-u` gives you a line
number instead of a usage message.

**Python.** The usual escape from bash, and a good one: real data
structures, `subprocess`, `pathlib`. What you keep is a runtime that
must be installed and might be 3.9, a `shell=True` footgun, and errors
that are exceptions — so the failure modes are still not in any
signature.

**Go.** Correct, fast, and one static binary — with a build step
between you and the answer, and no sum types, so failure modes are
`errors.Is` against sentinel values.

**Just / Make / Taskfile.** These do not replace the script; they
*invoke* it. The logic still ends up in a shell fragment somewhere.

**PowerShell.** The one mainstream shell with real objects, and it is
genuinely better than bash at exactly this. Its problem is the reverse
of Python's: excellent on Windows, an unusual choice everywhere else.

**Glide.** One file, `glide run`, no build step, and a checker that
runs whether you asked for it or not. The trade is that the standard
library is small (Chapter 31) — no regex yet, no flag parser, no
streaming — so a script that needs those is a script that waits.

---

### 5. Common Mistakes

**Translating bash line by line.** The bash script's structure is
dictated by bash's limits: everything is flat because there are no
return values worth having, and errors are `exit 1` because there is
nowhere else to put them. A line-by-line translation inherits all of
that. Write down the failure modes first — as a sum type — and the
structure follows.

**Reaching for `Error` because it is easier.** It is easier, and for a
twenty-line script it is right. For this one it would have deleted
`explain`, and with it the guarantee that every failure has exactly one
rendering.

**Forgetting that `?` needs a matching error type.** `fs.read_string`
returns `Result<_, Error>`, not `Result<_, ReleaseError>`. Inside a
function returning `ReleaseError` you must translate — with `match`, or
by declaring `ReleaseError.from`. The checker says so:

```
cannot propagate Error: this function returns Result<String, ReleaseError>,
and ReleaseError has no `from` that accepts it
```

**Using `os.exit` after opening something.** The staging directory is
created inside `stage`, which returns an `Err` rather than exiting —
that is what lets `errdefer` clean up. An `os.exit(1)` in there would
skip it and leave the half-built directory behind, which is the bash
behaviour being replaced.

**Assuming a non-zero exit is a failure.** `git status --porcelain`
exits 0 with output when the tree is dirty. `grep` exits 1 when it
finds nothing. `diff` exits 1 when files differ. If you write
`?` on the `Result` and never look at `status()`, you have written the
`set -e` bug in a new language.

**Leaving out the timeout.** It is one line, it covers every child in
the block, and it is the difference between a CI job that fails in 30
seconds and one that is still running in the morning.

---

### 6. Performance Considerations

**A script's runtime is dominated by the processes it starts.** This
one runs `git` three times and `shasum` once. The interpreter's
per-instruction cost — two orders of magnitude slower than Go
(Chapter 37) — is invisible next to four `fork`/`exec` pairs.

That is the general shape: **for a script, the interpreter's speed does
not matter, and the things it makes easy to avoid do.** Every external
process you replace with a string method is a four-order-of-magnitude
win. `status.lines().len()` versus `echo "$STATUS" | wc -l` is not
merely more correct, it is roughly ten thousand times faster, and the
bash version cannot do better because `wc` is a program.

**`process.run` buffers both streams in full.** Fine for `git
rev-parse`; wrong for `git log` on a large repository. Streaming is ○.

**Startup is a parse, a check, and a run.** No build step, no module
resolution, no init graph.

---

### 7. Best Practices

**Write the failure modes first.** Before any logic, list what can go
wrong as a sum type. It takes two minutes and it decides the structure
of everything after it.

**One function per external program, returning a Result.** `tool` and
`git` in this program. The two failure kinds — could not run, ran and
said no — get handled once, at the boundary, rather than at every call
site.

**Interpret a failure only where it has a second meaning.**
`rev-parse` failing means "not a repository", so translate it. `status`
failing means nothing more than itself, so propagate it. Translating
everything produces error messages that lie.

**Put a `scope(timeout:)` around anything that talks to the outside.**

**Use `errdefer`, not `defer`, for cleanup that should not happen on
success.** This is the single most common bash bug the language deletes
outright.

**Keep `main` thin.** Argument shape, one call, one `match` on the
result. Everything else is a function that returns a value.

**Do not add a `--verbose` flag.** Not until there is a `flag` module.
Positional arguments handled by a list pattern are complete and
checked; a hand-rolled flag loop is neither.

**Exit 2 for usage, 1 for failure, 0 for success.** It is the Unix
convention and it is what lets a caller tell a broken invocation from a
broken world.

---

### 8. Examples

#### The smallest useful case: three lines of bash

Not everything deserves the treatment above. This is the other end of
the range:

```bash
#!/usr/bin/env bash
set -e
for f in *.md; do
    echo "$f: $(wc -l < "$f") lines"
done
```

```glide-run
import fs
import os

fn main() -> Result<(), Error> {
    for name in fs.list_dir(os.cwd()?)? {
        if name.ends_with(".md") == false { continue }
        let body = fs.read_string(name)?
        println("{name}: {body.lines().len()} lines")
    }
    Ok(())
}
```

Nine lines against five, and the argument for the nine is: it does not
start a process per file, it does not break on a filename with a space,
`*.md` matching nothing does not iterate over the literal string
`*.md`, and a read failure says which file. Use `Error` here — there is
no `explain` function to earn a sum type.

#### Adding a failure mode, and letting the compiler find the work

Suppose the release script grows a rule: refuse to stage a version tag
that already exists.

```glide
type ReleaseError =
    NotSemver{ given: String }
    | NotARepo{ dir: String }
    | Dirty{ files: Int }
    | TagExists{ tag: String }                         // new
    | ToolFailed{ tool: String, status: Int, why: String }
```

```
release.gld:101:5: match is not exhaustive: TagExists not handled
 101 |     match e {
     |     ^^^^^
```

One diagnostic, pointing at `explain`, naming the variant. Add the arm
and the check:

```glide
        TagExists{ tag }   => "tag {tag} already exists"
```

```glide
fn check_tag(dir: String, version: String) -> Result<(), ReleaseError> {
    let tags = git(dir, ["tag", "--list", version])?
    if tags.trim() != "" {
        return Err(.TagExists{ tag: version })
    }
    Ok(())
}
```

The bash equivalent of this change is: add an `if`, add an `echo`, and
remember on your own that there is a place where failure messages are
supposed to be consistent.

#### The pattern this chapter is really about

```glide
// Every script that talks to the world has this shape.
fn main() {
    let args = os.args()
    let cfg = match args { … }               // 1. arguments, exhaustively

    match run(cfg) {                         // 2. one call
        Ok(v)  => report(v)                  // 3. success on stdout
        Err(e) => {                          // 4. failure on stderr, exit 1
            eprintln("prog: {explain(e)}")
            os.exit(1)
        }
    }
}
```

Four parts. The work is in `run`, which returns a value; `main` decides
what to *do* with a failure, and it is the only place that decides.
Bash cannot express the split because there is no value to return, so
every function ends up deciding for itself whether to print, exit, or
carry on — which is why a bash script's error handling is inconsistent
by construction rather than by neglect.

---

### 9. Summary & Exercises

**Summary**

- **A shell script's defects are type errors in disguise.** Word
  splitting, cleanup-on-every-path, swallowed tool failures, and
  off-by-one line counts are all questions bash cannot ask.
- **Write the failure modes first**, as a sum type. The structure of
  the program follows from that list, and `match` keeps the list
  honest.
- **`Err` means "could not run"; `status()` means "what it said".**
  Conflating them is the `set -e` bug, and it is available in every
  language.
- **`errdefer` is `trap … EXIT` done correctly** — the error path only,
  no flag variable, no `-E`.
- **`scope(timeout:)` kills children rather than abandoning them.** One
  line, and it is the difference between a CI job that fails and one
  that hangs.
- **Interpret a failure only where it has a second meaning**;
  propagate it otherwise.
- **The interpreter's speed does not matter for scripts**, and the
  processes it lets you avoid do — a string method beats a pipeline by
  four orders of magnitude and cannot be defeated by a filename.
- **Not every script deserves a sum type.** A program with a user wants
  its failures enumerated; a twenty-line one wants `Error`.
- The setup cost is the point: one file, `glide run`, no build step,
  and the checker runs anyway.

**Exercises**

1. **Find your own trap bug.** In any shell script you own with a
   `trap … EXIT`, work out whether the cleanup runs on success. If it
   does and it should not, you have found defect 3 in the wild. Now
   write the fix in bash, and count the lines.

2. **Port the smallest script you have.** Ten lines or fewer. Use
   `Error`, not a sum type. Time it. The interesting number is not how
   long the port took but how many behaviours you had to *decide* that
   bash had been deciding for you.

3. **Grow it until `Error` hurts.** Keep adding failure modes to the
   ported script until you want to distinguish two of them at a call
   site. That moment — where you reach for `e.find(…)` or wish you
   could `match` — is exactly the moment to convert to a sum type, and
   it is worth feeling once so you can recognise it later.

4. **Make the release script hang.** Add a `git` invocation that blocks
   (`git -C … fetch` from an unreachable host works, so does a credential
   prompt). Run it with the `scope(timeout:)` and without. Check with
   `ps` whether the child survives the program in each case.

5. **Argue for bash.** Write down the strongest case for leaving a
   script in bash. There is one, and it is not "it works" — it is about
   who else has to run it, and what is installed where. Then decide
   which of your own scripts it actually applies to.
