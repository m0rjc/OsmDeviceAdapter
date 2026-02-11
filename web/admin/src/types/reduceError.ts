/**
 * Derive a human-readable error message from an error object.
 * Most errors we encounter should be Error objects, but some libraries can throw other things.
 * 
 * @param error error object to derive a message from
 * @param fallback text to use if we cannot derive a message from the error object
 */
export function reduceError(error: any, fallback: string = "Unknown error") : string {
    if (error instanceof Error) {
        return error.message;
    }
    if (typeof error === 'string') {
        return error;
    }
    if (error instanceof Object) {
        if (error.message) {
            return error.message;
        }
    }
    try {
        return JSON.stringify(error);
    } catch (e) {
        console.error(e);
        return fallback;
    }
}