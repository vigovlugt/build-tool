# Makefile for the C project
CC = gcc
CFLAGS = -Wall -Wextra

# Target: main
main: util.o main.o
	$(CC) util.o main.o -o main

# Compile util.c to util.o
util.o: util.h util.c
	$(CC) $(CFLAGS) -c util.c

# Compile main.c to main.o
main.o: util.h main.c
	$(CC) $(CFLAGS) -c main.c

# Clean up build artifacts
clean:
	rm -f *.o main

# Run the program
run: main
	./main

.PHONY: clean run
