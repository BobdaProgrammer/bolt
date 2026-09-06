# The Syntax

### variables
<!-- Yes, I am using rust for the code blocks' language just because the syntax highlighting works well -->

```rust
//strings
let BestProgrammingLanguage = "bolt"

//ints
let NumberOfCats = 12

//bools
let IsBoltCool = true

//arrays
let SomeRandomProgrammingLanguages = ["Go", "Rust", "JS", "Python"]

//floats
let randomFloat = 9.2
```

### functions

```rust
fn fibonacci(x){
    if x == 0 {
        return 0
    } else {
        if x == 1 {
            return 1
        } else {
            fibonacci(x - 1) + fibonacci(x - 2)
        }
    }
}
```

this also shows the syntax for `if` and `else`, which brings us on to:

### if, else and else if
```javascript
let x = 3

if x==1 {
    log("My programming language doesn't work")
} else if x == 3 {
    log("My programming language works")
} else{
    log("My programming language doesn't work")
}
```
