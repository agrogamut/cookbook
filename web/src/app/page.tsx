import Link from "next/link";
import Image from "next/image";
import { BookOpen, MessageCircle, Utensils } from "lucide-react";
import { ConsultationForm } from "@/components/consultation-form";
import "./landing.css";

export default function LandingPage() {
  return (
    <div className="landing">
      <a className="landing-skip" href="#consultation">
        Skip to consultation form
      </a>
      <header className="landing-header">
        <Link className="wordmark" href="/" aria-label="MadamGY home">
          <Image
            src="/madamgy-logo.png"
            alt="MadamGY"
            width={204}
            height={41}
            priority
          />
          <span>Care for growing lives</span>
        </Link>
        <nav aria-label="Main navigation">
          <a href="#how-it-works">How it works</a>
          <Link href="/login">Staff sign in</Link>
        </nav>
      </header>
      <main>
        <div className="landing-hero">
          <div className="hero-copy">
            <p className="hero-context">Pediatric consultations with MadamGY</p>
            <h1>Growing up comes with questions.</h1>
            <p className="hero-intro">
              Start a conversation about your child’s food, growth and everyday
              care. Share a few details to request a consultation with a doctor.
            </p>
            <a className="mobile-consultation-link" href="#consultation">
              Request a consultation
            </a>
            <div className="consultation-topics">
              <div>
                <Utensils size={21} aria-hidden="true" />
                <p>
                  <strong>Food that fits their life</strong>
                  <span>
                    Discuss food preferences, feeding concerns and your child’s
                    needs.
                  </span>
                </p>
              </div>
              <div>
                <MessageCircle size={21} aria-hidden="true" />
                <p>
                  <strong>A conversation with a doctor</strong>
                  <span>
                    Share what you have noticed and the questions you want to
                    ask.
                  </span>
                </p>
              </div>
              <div>
                <BookOpen size={21} aria-hidden="true" />
                <p>
                  <strong>Something to take home</strong>
                  <span>
                    Your doctor can prepare books on daily care and recipes
                    using your child’s details.
                  </span>
                </p>
              </div>
            </div>
            <p className="hero-footnote">
              Tell us about your child. We’ll take it from there.
            </p>
          </div>
          <ConsultationForm />
        </div>
        <section
          className="how-section"
          id="how-it-works"
          aria-labelledby="how-title"
        >
          <div className="section-intro">
            <h2 id="how-title">
              A few details.
              <br />A more personal conversation.
            </h2>
            <p>You do not need to have every answer before you get in touch.</p>
          </div>
          <ol className="process-list">
            <li>
              <span className="process-number">1</span>
              <h3>Share the essentials</h3>
              <p>
                Tell us your name, your child’s name and date of birth, and how
                to reach you.
              </p>
            </li>
            <li>
              <span className="process-number">2</span>
              <h3>Connect with your doctor</h3>
              <p>
                The team assigns a doctor and contacts you to arrange your
                consultation. Online payment is optional.
              </p>
            </li>
            <li>
              <span className="process-number">3</span>
              <h3>Keep the guidance close</h3>
              <p>
                Your doctor can create daily-care and recipe books based on the
                information discussed during your consultation.
              </p>
            </li>
          </ol>
        </section>
        <section
          className="landing-questions"
          aria-labelledby="questions-title"
        >
          <h2 id="questions-title">Before you get started</h2>
          <div>
            <details>
              <summary>Do I need to pay to send a request?</summary>
              <p>
                No. Your details are saved first. When online payment is
                available, you can choose to pay for the doctor consultation
                after submitting the form.
              </p>
            </details>
            <details>
              <summary>Does submitting the form book a time?</summary>
              <p>
                It sends a consultation request to the MadamGY team. Staff will
                contact you to arrange the consultation. Paying online does not
                reserve an appointment time.
              </p>
            </details>
            <details>
              <summary>Do I need to create an account?</summary>
              <p>
                No account or password is needed. A phone number is required so
                the team can contact you. Email is optional.
              </p>
            </details>
          </div>
        </section>
      </main>
      <footer className="landing-footer">
        <Image src="/madamgy-logo.png" alt="MadamGY" width={145} height={29} />
        <p>A thoughtful place to begin your child’s next chapter.</p>
        <Link href="/login">Doctor and admin sign in</Link>
      </footer>
    </div>
  );
}
