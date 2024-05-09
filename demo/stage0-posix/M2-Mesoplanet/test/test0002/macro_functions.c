#define SUM(X,Y) (X) + (Y)
#define DOUBLE(X) SUM (X, X)

#define A (1)

int main() {
  return A + DOUBLE (2 * 3);
}
