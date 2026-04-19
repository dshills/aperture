// Context carries per-request metadata through the resolver chain.
export interface Context {
  userId: string;
  limit: number;
}
