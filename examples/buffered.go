package main

import "fmt"

func main() {
	ch := make(chan int, 1)
	ch <- -42
	close(ch)
	value, ok := <-ch
	fmt.Println(value)
	fmt.Println(ok)
	_, more := <-ch
	fmt.Println(more)
}
