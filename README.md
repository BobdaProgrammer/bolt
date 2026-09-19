# The Syntax

### Operators
- `+`
- `-`
- `*`
- `/`
- `>`
- `<`
- `>=`
- `<=`
- `==`
- `!=`
- `&&`
- `||`
- `!`
- `+=`
- `-=`
- `*=`
- `/=`


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

// Dictionaries/Maps/Hashes
let myDict = {1: 2, "Hello":"World", true:false}

// Multiple variables (only works when value is spread - we'll get onto spreads later)
let [x,y] = funcThatReturnsMultipleValues()
```

### functions

```rust
let fibonacci = fn (x){
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
```go
let x = 3

if x==1 || x == 12 {
    log("My programming language doesn't work")
} else if x == 3 && x != 100 {
    log("My programming language works")
} else{
    log("My programming language doesn't work")
}
```

### Loops
In bolt, you can loop over arrays, maps, numbers (it will run the number amount of times) and strings. For arrays and strings, the first value in the for is the index and the second is the value. For maps, the first variable is the key and the second is the value. for numbers you can only have 1 variable: `for x = range 10` which is the index (still starts at 0) 
```go
let myArr = [0,1,2,3]
for ind, x = range myArr{
    log("Index:",ind,"Value:", x)
}

let a = true
for a {
    if 2==2{
        a = false
    }
}
```

### Structs

```javascript
struct Person{
    name,
    age
}

let bob = new Person{name:"bob"}
bob.age = 24

log(bob) // {name: "bob", age: 24}
```

you can also do struct casting using the `as` keyword
```rust
struct Person{
    name,
    age
}

struct Animal{
    name
}

let bob = new Animal{name: "bob"}
let bobPerson = bob as Person
bobPerson.age = 20

log(typeof(bob)) // Animal
log(typeof(bobPerson)) // Person
```

### Arrays
```javascript
let myArray = [1,2,3,4,5]

let anIndex = myArray[2] // 3

let aSlice = myArray[0:3] // 1,2,3

myArray = push(myArray, 6)

log(myArray) // [1,2,3,4,5,6]

log(len(myArray)) // 6

myArray[2] = 10
log(myArray) // [1,2,10,4,5,6]

log([1,2,3][1]) // 2
```
Note that when slicing. It is similar to most languages where the first index of the slice in included and the last is excluded. i.e. `myArr[0:4]` would include the 0 index and exclude the 4th index leading to giving you the first 4 elements (0, 1, 2, 3 indexes) 

As a quick sidenote you can also use indexing and slicing on strings:
```go
let mystr = "Hello World"
log(mystr[0]) // H
log(mystr[0:5]) // Hello
```

### Spreads
Spreads are an interesting part of bolt and they work using the `...`. They work similarly to other languages where an array can be turned into multiple values. It will make more sense when you check out the code. First let's start with what you **shouldn't** do and you will see the problem:
```go
// THIS IS WHAT YOU **SHOULDN'T** DO
let smallArray = [9,10,11]
let largeArray = [1,2,3,4,5,6,7,8,smallArray]

log(largeArray) // [1,2,3,4,5,6,7,8,[9,10,11]]
```
What has happened is instead of adding the values of small array to large array, it has just made the element its own array object that contains the small array elements.
Here is how to fix this
```go
let smallArray = [9,10,11]
let largeArray [1,2,3,4,5,6,7,8, ...smallArray] // this creates a spread of small array

log(largeArray) // [1,2,3,4,5,6,7,8,9,10,11]
```
We can now see that large array is now a full list from 1 to 11 since the values of small array were injected into large array instead of being stored as an array in an array.

There is more to this, but before we dive into the possibilities let's take a peek under the hood at something:
```javascript
let multipleReturnVals = fn(){
    return "this is the first return", "this is the second return"
}

let [x, y] = multipleReturnVals()
```
The important part here is that when the function returns multiple values, under the hood it is actually returning a **spread**.
You might now see where I am going with this:
```javascript
let arr = [1,2,3]

let [a, b, c] = ...arr
```
By using a **spread** of the array, I can then use a multiple value let statement to assign the values in the array to variables.
An important thing to note is that **multiple value let statements only work with spreads**.

Some more possibilities:
```go
let arr1 = [1,2,3]
let arr2 = [-2, -1, 0]

arr2 = push(arr2, ...arr1)
log(arr2) //[-2, -1, 0, 1, 2, 3]

let funcWithArrays = fn(x){return [x,x+1,x+2]}
let [a, b, c] = ...funcWithArrays(1) // the function returns an array and we can turn it to a spread to let us use the array values in the assignments
```

One of the best things you can do with this is **use a spread to give multiple function arguments**
```go
let myFunc = fn(a, b, c){return a+b+c}

let argumentArray = [10,15,20]
log(myFunc(...argumentArray)) // This will inject 10, 15, 20 as the function arguments and so the function will return 45
```

### Blank identifier
This is pretty simple. You can use `_` as a placeholder blank object that you don't need to access. It is mainly used in for loops or variable initialisations of a multiple return value function.
```go
// we use the _ to say that we don't need the index but we still need a place holder to tell bolt to use x as the value
for _, x = range myArr{
    log(x) // value
}

let myfn = fn(x){return x, x+1, x+2}

let [_, a, b] = myfn(2) // The function will return 2, 3, 4, here we say that we don't need the first value (2) but want the others so we can use the _ as a placeholder
```

### Comments
Comments are the same as you would do in most languages: `//` followed by the comment, newline ends the comment. No multi line comments

### Importing
You can import other bolt files like this
```
import "math.bolt"

log(math.sqrt(25))
```
Bolt would look for a file called `math.bolt` and it would then load it and it is then imported into the current file with all of its data accessable through the namespace `math`. The namespace is defined based on the **filename**. e.g. test.bolt -> test utils/arrays.bolt -> arrays

## Builtins
### len
`len` gets the length of arrays, strings and hashes. It returns an integer and only expects one value
```go
len("hello") // 5
len([1,2,3,4]) // 4
len({"name": "bob", 12:15}) // 2
```

### push
`push` returns a new array of the array given with the value given added to the end:
```javascript
let myArr = [1,2,3,4]
myArr = push(myArr, 5)
log(myArr) // [1,2,3,4,5]

newArr = push(myArr, 6)
log(newArr) // [1,2,3,4,5,6]
log(myArr) // [1,2,3,4,5]
```

### delete
`delete` removes a value from a map or array:
```go
let myMap = {"val1":2, 2:3}
delete(myMap, "val1")
log(myMap) // {2:3}

let myArr = [1,2,3,"hello",2.2]
delete(myArr, "hello")
log(myArr) // [1,2,3,2.2]
delete(myArr, 2.2)
log(myArr) // [1,2,3]
```

### panic
`panic` exits the program immediately and prints out an error message:
```go
panic("This has failed badly") // The program will immediately exit with PANIC: This has failed badly - LINE=1 
```

### log
`log` prints out whatever you give it in one line. You have seen this a lot before. You can give it infinate arguments of any object type:
```javascript
log("hi", 2, {"name":"bob"}, [1,2,3], " , ", "another val")
```

### input
`input` will wait for the user to press enter and log everythign the user tyoes and return it as a string. It can also give a prompt
``` javascript
let name = input("What is your name? ") // this will ask 'What is your name? ' and it will wait until the user presses enter, if the user enters 'Bob', input would return a string of 'Bob'
```

### keys
`keys` returns an array of all the keys in a map
```javascript
let myMap = {"name":"bob", "age": 24}
log(keys(myMap)) // ["name", "age"]
```

### typeof
`typeof` returns a string of the objects type
```javascript
typeof(2) // INT
typeof(2.2) // FLOAT
typeof("hi") // STRING
typeof([1,2,3]) // ARRAY
typeof({"val1":2}) // HASH

struct Person{name, age}
typeof(Person) // Struct
let bob = new Person{name:"bob", age:24}
typeof(bob) // Person
```
it is to note that if you do `typeof` on a struct instance, you get the name of the struct type. See above.

### string
`string` casts the argument given to a string
```go
string(2) // "2"
string(2.2) // "2.2"
string(true) // "true"
```

### int
`int` casts the argument given to a int
```go
int("2") // 2
int(2.9) // 2
int(true) // 1
```

### float
`float` casts the argument given to a float
```go
float("2.2") // 2.2
float(2) // 2.0
float(true) // 1.0
```

