#include <stdio.h>
#include "util.h"

int add(int a, int b)
{
    return a + b;
}

int multiply(int a, int b)
{
    return a * b;
}

void print_greeting(const char *name)
{
    printf("Hello, %s! Welcome to the C build system example.\n", name);
}