#include <stdio.h>
#include "util.h"

int main()
{
    // Use the utility functions
    print_greeting("Build System User ");

    int x = 10;
    int y = 5;

    int sum = add(x, y);
    int product = multiply(x, y);

    printf("\nCalculations:\n");
    printf("%d + %d = %d\n", x, y, sum);
    printf("%d * %d = %d\n", x, y, product);

    return 0;
}
