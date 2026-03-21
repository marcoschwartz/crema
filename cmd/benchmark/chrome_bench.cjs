// Chrome headless benchmark — same URLs as crema
const puppeteer = require('puppeteer-core');

(async () => {
    const results = [];
    const browser = await puppeteer.connect({
        browserWSEndpoint: process.argv[2] || 'ws://localhost:9223/devtools/browser'
    });

    const urls = [
        'https://example.com',
        'https://news.ycombinator.com',
        'https://httpbin.org/html',
        'https://jsonplaceholder.typicode.com',
    ];

    for (const url of urls) {
        const page = await browser.newPage();
        const start = Date.now();
        try {
            await page.goto(url, { waitUntil: 'load', timeout: 30000 });
        } catch(e) {
            console.log(JSON.stringify({ url, error: e.message }));
            await page.close();
            continue;
        }
        const navTime = Date.now() - start;

        const title = await page.title();
        const links = await page.$$eval('a', els => els.length);

        // Screenshot
        const ssStart = Date.now();
        await page.screenshot({ path: '/tmp/chrome_bench_ss.png' });
        const ssTime = Date.now() - ssStart;

        results.push({ url, navTime, ssTime, title, links });
        await page.close();
    }

    // Output results as JSON
    console.log(JSON.stringify(results));
    await browser.disconnect();
})();
