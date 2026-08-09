# Glide By Example
Glide examples, starting from simple and working up, showing idiomatic Glide code.

Every fenced block here is run against the interpreter by `go test`
(see `glide/doc_examples_test.go`). Comment lines at the *end* of a
block are checked: `// error: …` means the program must fail with
exactly that error; other trailing comments are the expected output.

As simple as it gets. Hello World !!!
No imports needed. the entry point to any glide program has to be the main() function
```rust
fn main() {
    println("Hello, Glide!")
}
// Hello, Glide!
```

With a variable. Glide is strongly typed, but type is inferred by a literal.

There is really only 2 forms of print. `print` and `println` (`ln` just adds a new line at the end)

No printf. Everything you need is interpolated within `print`/`println`
(eprint/eprintln work the same way, but output to stderr rather than stdout)
```rust
fn main() {
    let name = "Glide"
    println("Hello {name}!")
}
```

You can be explicit if you want. Sometimes it's required, but not here.
```rust
fn main() {
    let name: String = "Glide"
    println("Hello {name}!")
}
```

Variables are immutable by default. They can't be changed after creation.
```rust
fn main() {
    let count = 1
    count = count + 1
    println(count)
}
// error: line 3: cannot mutate through immutable binding "count" (declare it with `let mut`)
```

Make variables mutable (changeable) with the `mut` keyword.
Add in the standard shortcut for adding to a variable
```rust
fn main() {
    let mut count = 1
    count += 1
    println(count)
}
// 2
```

You can reassign (or shadow) a variable within the same scope, but not in another scope (shown later)
```rust
fn main() {
    let count = 1
    let count = count + 2
    println(count)
    let count = "three"
    println(count)
}
// 3
// three
```

## Functions
```rust
fn main() {
	let name = "Glide"
	greet()
}

// greet doesn't accept or return any values.
fn greet() {
	print("Hello ")
	println("Glide!")
}
// Hello Glide!
```

Pass a variable to a function.
The functions can be in any order. You can call a function not yet defined.

```rust
fn main() {
	greet("Glide")
}
// Function definition has to contain the type of parameter it receives
fn greet(name: String) {
	print("Hello ")
	println("{name}!")
}
```

## Closures

`fn` declarations are top level only. But a closure is an *expression* —
a function you can create anywhere, bind to a variable, and pass around.
Written with pipes around the parameters: `|x|`, or `||` for none.

A closure can see the variables of the scope it was created in.
```rust
fn main() {
	let greeting = "Hello"
	let greet = |name| {
		println("{greeting} {name}!")
	}
	greet("Glide")
	greet("closures")
}
// Hello Glide!
// Hello closures!
```

A one-expression body needs no braces, and its result is returned.
Captured variables are shared, not copied: mutating a captured
`let mut` is visible outside the closure.
```rust
fn main() {
	let double = |n| n * 2
	println(double(21))

	let mut total = 0
	let add = |n| { total += n }
	add(3)
	add(4)
	println(total)
}
// 42
// 7
```

## If

`if` is an expression — it produces a value, so there is no ternary
operator: `if` *is* the ternary, and it reads the same at two lines or
ten. Conditions take no parentheses; braces are always required.
```rust
fn main() {
    let n = 7
    let sign = if n > 0 { "positive" } else if n < 0 { "negative" } else { "zero" }
    println(sign)
}
// positive
```

The condition must be a Bool. There is no truthiness — `if 1 { }` is
an error, so the classic `if x = 1` / `if 0` bug families don't exist.
```rust
fn main() {
    if 1 { println("never") }
}
// error: line 2: if condition must be Bool, got Int
```

## Loops

One loop keyword: `for`. Three forms — forever, while-style, and
over the elements of anything iterable. `break` and `continue` do
what Go's do, and using them outside a loop is caught before the
program runs.
```rust
fn main() {
    // Forever, until break.
    let mut n = 1
    for {
        n *= 2
        if n > 100 { break }
    }
    println(n)

    // While-style: loop while the condition holds.
    let mut k = 0
    for k < 3 {
        k += 1
    }
    println(k)

    // Over a range (half-open: 0, 1, 2) — with continue to skip.
    for i in 0..3 {
        if i == 1 { continue }
        println(i)
    }
}
// 128
// 3
// 0
// 2
```

## Lists

Lists grow with `push`, which — like all mutation — needs a `mut`
binding. Indexing panics out of bounds: an invalid index is a bug in
the program, not a condition to handle.
```rust
fn main() {
    let mut primes = [2, 3, 5]
    primes.push(7)
    println(primes.len())
    println(primes[3])

    primes[0] = 11
    primes[0] += 2
    println(primes[0])
}
// 4
// 7
// 13
```

`repeat` is the fill constructor: n slots, value named explicitly —
Glide has no zero values, so there is no `make([]int, n)` filling
things in behind your back.
```rust
fn main() {
    let board = ["."].repeat(3)
    println(board.len())
    println(board[2])
}
// 3
// .
```

`sorted` returns a new list and works on any immutable binding;
`sort_by` sorts in place, so it needs `mut`.
```rust
fn main() {
    let xs = [3, 1, 2]
    println(xs.sorted()[0])
    println(xs[0])
}
// 1
// 3
```

The naming rule holds across the whole set: a past participle hands
back a new list, a verb changes this one. So `reversed` and `slice`
copy, while `insert`, `remove` and `extend` need `mut`.
```rust
fn main() {
    let mut xs = [1, 2, 3]
    println(xs.reversed())
    println(xs.slice(0, 2))

    xs.insert(0, 0)
    xs.extend([4, 5])
    println(xs)
    println(xs.remove(0))
    println(xs)
}
// [3, 2, 1]
// [1, 2]
// [0, 1, 2, 3, 4, 5]
// 0
// [1, 2, 3, 4, 5]
```

Asking a list about itself gives Options where the empty case is a
real answer, and traps where you named a slot that isn't there.
`pop` on an empty list is the loop condition of every worklist
algorithm; `xs[5]` on a three-element list is a bug.
```rust
fn main() {
    let mut xs = [10, 20]
    println(xs.contains(20))
    println(xs.index_of(99))
    println(xs.first())
    println(xs.pop())
    println(xs.pop())
    println(xs.pop())
}
// true
// None
// Some(10)
// Some(20)
// Some(10)
// None
```

## Maps

Map literals use square brackets. Reading a missing key isn't an
error and isn't a zero value — you get an Option back, and `??`
supplies the default. Iteration order is insertion order, always.
```rust
fn main() {
    let mut stock = ["apples": 4, "pears": 2]
    stock["plums"] = 10

    println(stock["apples"] ?? 0)
    println(stock["mangoes"] ?? 0)

    for (name, count) in stock {
        println("{name}: {count}")
    }
}
// 4
// 0
// apples: 4
// pears: 2
// plums: 10
```

`if let` unwraps the Option when you want to *do* something rather
than default: the binding exists only inside the braces.
```rust
fn main() {
    let menu = ["tea": 3]
    if let price = menu["tea"] {
        println("tea costs {price}")
    }
    if let price = menu["coffee"] {
        println("coffee costs {price}")
    } else {
        println("no coffee here")
    }
}
// tea costs 3
// no coffee here
```

`keys` and `values` come back in insertion order and line up with each
other. `remove` hands you what was there and drops the key from the
order — so re-inserting it later puts it at the end, not back where it
was.
```rust
fn main() {
    let mut stock = ["apples": 4, "pears": 2, "plums": 10]
    println(stock.keys())
    println(stock.values())
    println(stock.contains_key("pears"))

    println(stock.remove("pears"))
    println(stock.remove("pears"))
    stock["pears"] = 99
    println(stock)
}
// ["apples", "pears", "plums"]
// [4, 2, 10]
// true
// Some(2)
// None
// ["apples": 4, "plums": 10, "pears": 99]
```

## Match

`match` is the only multi-way branch, and it unifies Go's switch with
pattern matching. Arms take multiple values, ranges (half-open, like
`..` everywhere), and guards. No fallthrough exists, so no `break`
noise either. It's an expression, like `if`.
```rust
fn describe(code: Int) -> String {
    match code {
        200        => "ok"
        301, 302   => "redirect"
        400..500   => "client error"
        n if n < 0 => "not a status code"
        _          => "something else"
    }
}
fn main() {
    println(describe(200))
    println(describe(302))
    println(describe(418))
    println(describe(0 - 1))
}
// ok
// redirect
// client error
// not a status code
```

Strings match by equality — no regex smuggled into patterns.
```rust
fn main() {
    let verb = "POST"
    println(match verb {
        "GET"         => "read"
        "PUT", "POST" => "write"
        _             => "unknown"
    })
}
// write
```

Drop the subject and `match` becomes Go's expressionless switch: each
arm is a condition, the first true one wins. This replaces long
`else if` chains.
```rust
fn main() {
    let score = 87
    let grade = match {
        score >= 90 => "A"
        score >= 80 => "B"
        score >= 70 => "C"
        _           => "F"
    }
    println(grade)
}
// B
```

Arms are separated by a newline or a comma, whichever reads better. A
comma lets the whole thing sit on one line.
```rust
fn main() {
    let n = 2
    println(match n { 1 => "one", 2 => "two", _ => "many" })
}
// two
```

## Running other programs

`process.run` takes an executable and a list of arguments. There is no
shell, so nothing is word-split and there is nothing to quote — an
argument containing a space stays one argument.

The important part is what counts as an error. `Err` means the program
could not be started at all. A program that ran and exited non-zero
returns `Ok`: exiting 1 is how `grep` says "no match" and `test` says
"false", and if that were an `Err` then `?` would propagate an ordinary
answer.
```rust
import process

fn main() {
    match process.run("echo", ["a b", "c"]) {
        Ok(out) => println("[{out.status()}] {out.stdout().trim()}")
        Err(e)  => println("could not run echo: {e}")
    }

    match process.run("sh", ["-c", "exit 3"]) {
        Ok(out) => println("ran, exited {out.status()}, ok={out.ok()}")
        Err(e)  => println("could not run sh: {e}")
    }

    match process.run("no-such-program-6a1f") {
        Ok(out) => println("unexpected {out.status()}")
        Err(e)  => println("that one really is an error")
    }
}
// [0] a b c
// ran, exited 3, ok=false
// that one really is an error
```

A child is bound to its scope, like every other blocking operation. If
the scope's timeout fires, the process is killed — not merely
abandoned.
```rust
import process
import time

fn main() {
    let start = time.now()
    scope(timeout: 200.ms) s {
        let t = s.spawn(|| process.run("sleep", ["30"]))
        println("never reached: {t.join()}")
    }
    println("back in {time.now() - start < 5.s}")
}
// back in true
```

## Files and the environment

Anything that can fail returns a `Result`, so `fn main() -> Result<(),
Error>` plus `?` replaces `set -e`: a failed step prints one line and
exits 1. The two predicates, `exists` and `is_dir`, return a plain
`Bool` — a `Result` there would be one you could only ever unwrap.
```rust
import fs
import os

fn main() -> Result<(), Error> {
    let dir = fs.join([os.env("TMPDIR") ?? "/tmp", "byexample"])
    fs.mkdir_all(dir)?

    let path = fs.join([dir, "notes.txt"])
    fs.write_string(path, "first\n")?
    fs.append_string(path, "second\n")?

    println(fs.exists(path))
    println(fs.read_string(path)?.lines())
    println(fs.list_dir(dir)?)

    fs.remove_all(dir)?
    println(fs.exists(dir))
    Ok(())
}
// true
// ["first", "second"]
// ["notes.txt"]
// false
```

`os.env` gives an Option rather than an empty string, because a
variable that is set to nothing is not the same as one that isn't set.
```rust
import os

fn main() {
    println(os.env("GLIDE_NOT_SET_1234") ?? "(unset)")
    println((os.env("PATH") ?? "").len() > 0)
}
// (unset)
// true
```

## Arithmetic

`abs`, `min`, `max` and `pow` are methods on the number, not a `math`
module — `cmp` already lives there, and an import in front of
`n.abs()` buys nothing. The result keeps the receiver's own width.
```rust
fn main() {
    println((0 - 7).abs())
    println(5.min(3))
    println(5.max(3))
    println(2.pow(10))
}
// 7
// 3
// 5
// 1024
```

Overflow traps here as it does everywhere else: a signed type's
minimum has no positive counterpart, so `abs` refuses rather than
quietly handing back the negative number.
```rust
fn main() {
    let n: i8 = -128
    println(n.abs())
}
// error: line 3: i8 overflow: abs of -128 (use wrapping_neg for modular arithmetic)
```

The dividing line against the `math` module is **width**. Anything that
has to work at every numeric width is a method, because the receiver is
what says which width. Anything Float-only lives in `math`, along with
the constants — which could never have been methods on a number at all.
```rust
import math

fn main() {
    let f = 0.0 - 2.5
    println(f.abs())            // works at every width -> method
    println(math.floor(f))      // Float-only -> module
    println(math.ceil(f))
    println(math.round(f))
    println(Int(math.trunc(f)))
    println(math.sqrt(9.0))
    println(math.pi)
}
// 2.5
// -3
// -2
// -3
// -2
// 3
// 3.141592653589793
```

NaN is a value, so testing for one is a call and never a comparison —
`x == math.nan` is false by IEEE 754 and always will be.
```rust
import math

fn main() {
    println(math.is_nan(math.nan))
    println(math.nan == math.nan)
    println(math.is_finite(1.0))
    println(math.is_infinite(math.inf))
}
// true
// false
// true
// true
```

Reach for one of these as a method and the error says where it went,
rather than just saying no.
```rust
fn main() {
    println(5.sqrt())
}
// error: line 2: sqrt lives in the math module and takes a Float — write math.sqrt(Float(n))
```

## Equality

`==` is structural and universal — there is no trait to declare and no
way to redefine it. It recurses through everything with structure:
structs, variants, tuples, lists, maps, `Result`, `Option`.
```rust
fn main() {
    println([[1], [2, 3]] == [[1], [2, 3]])
    println((1, "a") == (1, "a"))
    println(Ok(Some(1)) == Ok(Some(1)))
    println(Some(None) == None)
}
// true
// true
// true
// false
```

A **Map ignores insertion order.** Order is what you get when you
*iterate* a map, not part of what the map is — two maps holding the
same pairs are the same map however they were built. A List stays
order-sensitive, because a list is a sequence and a map is a set of
pairs.
```rust
fn main() {
    let mut a = ["x": 1, "y": 2]
    let b = ["y": 2, "x": 1]
    println(a == b)
    println(a.keys() == b.keys())

    // Removing and re-adding moves the key to the end of the
    // iteration order, and still doesn't change what the map is.
    let _ = a.remove("x")
    a["x"] = 1
    println(a.keys())
    println(a == b)
}
// true
// false
// ["y", "x"]
// true
```

## Errors

`Error` is the dynamic error type application code returns. Anything is
assignable to it, so raising one needs no ceremony — and `?` propagates
a callee's error into it with no conversion to write.
```rust
fn parse_port(s: String) -> Result<Int, Error> {
    match s.parse_int() {
        Some(n) => if n > 0 && n < 65536 { Ok(n) } else { Err("port out of range: {n}") }
        None    => Err("not a number: {s}")
    }
}

fn main() {
    println(parse_port("8080"))
    println(parse_port("99999"))
    println(parse_port("http"))
}
// Ok(8080)
// Err("port out of range: 99999")
// Err("not a number: http")
```

Note the quotes. `Error` is *erased* — the slot holds whatever it was
given, here a String — so printing the whole `Result` shows it as the
String it is, while a host error prints bare. Unwrap it and the
difference goes away, because interpolation is what you actually use:
```rust
fn main() {
    match parse_port("http") {
        Ok(n)  => println("port {n}")
        Err(e) => println("failed: {e}")
    }
}

fn parse_port(s: String) -> Result<Int, Error> {
    match s.parse_int() {
        Some(n) => Ok(n)
        None    => Err("not a number: {s}")
    }
}
// failed: not a number: http
```

`context` adds a breadcrumb to an `Err` and passes an `Ok` through, so
a chain of `?` reads as a trail rather than one bare message. Give
`main` a `Result` return and `?` becomes the whole error path — no
`set -e`, no `if err != nil`.
```rust
import fs

fn load(path: String) -> Result<Int, Error> {
    let body = fs.read_string(path).context("reading {path}")?
    parse_port(body.trim())
}

fn parse_port(s: String) -> Result<Int, Error> {
    match s.parse_int() {
        Some(n) => Ok(n)
        None    => Err("not a number: {s}")
    }
}

fn main() -> Result<(), Error> {
    println(load("/nonexistent/config"))
    Ok(())
}
// Err(reading /nonexistent/config: open /nonexistent/config: no such file or directory)
```

A library defines its failure modes as a sum type instead, so callers
can match them exhaustively and adding a case breaks them at compile
time. `?` still converts one into an `Error` at any application
boundary.
```rust
type PortErr = NotANumber(String) | OutOfRange(Int)

fn strict(s: String) -> Result<Int, PortErr> {
    let Some(n) = s.parse_int() else { return Err(.NotANumber(s)) }
    if n <= 0 || n >= 65536 { return Err(.OutOfRange(n)) }
    Ok(n)
}

fn main() {
    match strict("99999") {
        Ok(n)              => println("port {n}")
        Err(NotANumber(s)) => println("not a number: {s}")
        Err(OutOfRange(n)) => println("out of range: {n}")
    }
}
// out of range: 99999
```
