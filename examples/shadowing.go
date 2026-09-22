package main

import "fmt"

func main() {
	ch := make(chan int, 1)
	ch <- 1
	go func() {
		ch := make(chan int, 1)
		ch <- 2
		value := <-ch
		fmt.Println(value)
	}()
	value := <-ch
	fmt.Println(value)
}
