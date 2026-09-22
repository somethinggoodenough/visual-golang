package main

import "fmt"

func main() {
	ch := make(chan int)
	go func() {
		go func() {
			ch <- 42
		}()
	}()
	value, _ := <-ch
	fmt.Println(value)
}
